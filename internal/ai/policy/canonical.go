package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxCanonicalNumberLength = 10_000

// ErrInvalidCanonicalValue 表示输入无法安全映射为唯一 JSON 表示。
var ErrInvalidCanonicalValue = errors.New("invalid canonical JSON value")

// CanonicalJSON 将 JSON 兼容值编码为 SentinelOps 的稳定身份表示。
//
// 对象键按字节序排列；等价数字统一为无指数十进制；RFC3339 时间统一为
// UTC RFC3339Nano；nil、空数组、空对象和空字符串保持各自唯一表示。
func CanonicalJSON(value any) ([]byte, error) {
	if err := validateCanonicalInput(reflect.ValueOf(value), make(map[visit]bool)); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical input: %w", errors.Join(ErrInvalidCanonicalValue, err))
	}
	decoded, err := decodeJSON(raw)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 0, len(raw))
	result, err = appendCanonical(result, decoded)
	if err != nil {
		return nil, err
	}
	return result, nil
}

type visit struct {
	typ reflect.Type
	ptr uintptr
}

func validateCanonicalInput(value reflect.Value, seen map[visit]bool) error {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		if value.Kind() == reflect.Pointer {
			key := visit{typ: value.Type(), ptr: value.Pointer()}
			if seen[key] {
				return fmt.Errorf("cyclic pointer: %w", ErrInvalidCanonicalValue)
			}
			seen[key] = true
			defer delete(seen, key)
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return fmt.Errorf("string is not valid UTF-8: %w", ErrInvalidCanonicalValue)
		}
	case reflect.Float32, reflect.Float64:
		floating := value.Float()
		if value.Kind() == reflect.Float32 {
			floating = float64(float32(floating))
		}
		if strconv.FormatFloat(floating, 'g', -1, value.Type().Bits()) == "+Inf" ||
			strconv.FormatFloat(floating, 'g', -1, value.Type().Bits()) == "-Inf" ||
			strconv.FormatFloat(floating, 'g', -1, value.Type().Bits()) == "NaN" {
			return fmt.Errorf("non-finite number: %w", ErrInvalidCanonicalValue)
		}
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("map key type %s is not string: %w", value.Type().Key(), ErrInvalidCanonicalValue)
		}
		if value.IsNil() {
			return nil
		}
		key := visit{typ: value.Type(), ptr: value.Pointer()}
		if seen[key] {
			return fmt.Errorf("cyclic map: %w", ErrInvalidCanonicalValue)
		}
		seen[key] = true
		defer delete(seen, key)
		iterator := value.MapRange()
		for iterator.Next() {
			if !utf8.ValidString(iterator.Key().String()) {
				return fmt.Errorf("map key is not valid UTF-8: %w", ErrInvalidCanonicalValue)
			}
			if err := validateCanonicalInput(iterator.Value(), seen); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if value.IsNil() {
			return nil
		}
		key := visit{typ: value.Type(), ptr: value.Pointer()}
		if seen[key] {
			return fmt.Errorf("cyclic slice: %w", ErrInvalidCanonicalValue)
		}
		seen[key] = true
		defer delete(seen, key)
		for index := 0; index < value.Len(); index++ {
			if err := validateCanonicalInput(value.Index(index), seen); err != nil {
				return err
			}
		}
	case reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if err := validateCanonicalInput(value.Index(index), seen); err != nil {
				return err
			}
		}
	case reflect.Struct:
		if value.Type() == reflect.TypeFor[time.Time]() {
			return nil
		}
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if field.PkgPath == "" {
				if err := validateCanonicalInput(value.Field(index), seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func decodeJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder)
	if err != nil {
		return nil, fmt.Errorf("decode canonical input: %w", errors.Join(ErrInvalidCanonicalValue, err))
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values: %w", ErrInvalidCanonicalValue)
		}
		return nil, fmt.Errorf("finish canonical input: %w", errors.Join(ErrInvalidCanonicalValue, err))
	}
	return value, nil
}

func decodeJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}

	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string")
			}
			if _, duplicate := object[key]; duplicate {
				return nil, fmt.Errorf("duplicate object key %q", key)
			}
			value, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
			return nil, fmt.Errorf("unterminated object")
		}
		return object, nil
	case '[':
		var array []any
		for decoder.More() {
			value, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
			return nil, fmt.Errorf("unterminated array")
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %q", delimiter)
	}
}

func appendCanonical(destination []byte, value any) ([]byte, error) {
	switch typed := value.(type) {
	case nil:
		return append(destination, "null"...), nil
	case bool:
		return strconv.AppendBool(destination, typed), nil
	case string:
		if timestamp, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			typed = timestamp.UTC().Format(time.RFC3339Nano)
		}
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		return append(destination, encoded...), nil
	case json.Number:
		number, err := canonicalNumber(string(typed))
		if err != nil {
			return nil, err
		}
		return append(destination, number...), nil
	case []any:
		destination = append(destination, '[')
		for index, item := range typed {
			if index > 0 {
				destination = append(destination, ',')
			}
			var err error
			destination, err = appendCanonical(destination, item)
			if err != nil {
				return nil, err
			}
		}
		return append(destination, ']'), nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		destination = append(destination, '{')
		for index, key := range keys {
			if index > 0 {
				destination = append(destination, ',')
			}
			encodedKey, _ := json.Marshal(key)
			destination = append(destination, encodedKey...)
			destination = append(destination, ':')
			var err error
			destination, err = appendCanonical(destination, typed[key])
			if err != nil {
				return nil, err
			}
		}
		return append(destination, '}'), nil
	default:
		return nil, fmt.Errorf("unsupported decoded type %T: %w", value, ErrInvalidCanonicalValue)
	}
}

func canonicalNumber(raw string) (string, error) {
	negative := strings.HasPrefix(raw, "-")
	unsigned := strings.TrimPrefix(raw, "-")
	mantissa, exponentText, hasExponent := unsigned, "", false
	if index := strings.IndexAny(unsigned, "eE"); index >= 0 {
		mantissa, exponentText, hasExponent = unsigned[:index], unsigned[index+1:], true
	}
	exponent := 0
	if hasExponent {
		if len(exponentText) > 7 {
			return "", fmt.Errorf("number exponent is too large: %w", ErrInvalidCanonicalValue)
		}
		parsed, err := strconv.Atoi(exponentText)
		if err != nil || parsed > maxCanonicalNumberLength || parsed < -maxCanonicalNumberLength {
			return "", fmt.Errorf("number exponent is out of bounds: %w", ErrInvalidCanonicalValue)
		}
		exponent = parsed
	}

	integerPart, fractionalPart := mantissa, ""
	if index := strings.IndexByte(mantissa, '.'); index >= 0 {
		integerPart, fractionalPart = mantissa[:index], mantissa[index+1:]
	}
	digits := integerPart + fractionalPart
	leadingZeroes := len(digits) - len(strings.TrimLeft(digits, "0"))
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return "0", nil
	}
	decimalPosition := len(integerPart) + exponent - leadingZeroes
	digits = strings.TrimRight(digits, "0")
	if len(digits)+abs(decimalPosition) > maxCanonicalNumberLength {
		return "", fmt.Errorf("canonical number is too large: %w", ErrInvalidCanonicalValue)
	}

	var builder strings.Builder
	if negative {
		builder.WriteByte('-')
	}
	switch {
	case decimalPosition <= 0:
		builder.WriteString("0.")
		builder.WriteString(strings.Repeat("0", -decimalPosition))
		builder.WriteString(digits)
	case decimalPosition >= len(digits):
		builder.WriteString(digits)
		builder.WriteString(strings.Repeat("0", decimalPosition-len(digits)))
	default:
		builder.WriteString(digits[:decimalPosition])
		builder.WriteByte('.')
		builder.WriteString(digits[decimalPosition:])
	}
	return builder.String(), nil
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
