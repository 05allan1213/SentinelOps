package memory

// ChatInlineCompress Chat Pipeline 内联上下文压缩用户提示词（Token 超限时触发）。
// 使用方式：System=SummarySystem，User=ChatInlineCompress + historyText
// 对应代码：chat_pipeline/lambda_func.go summarizeOldHistory()
// 区别于 SummaryUser：输出纯文本（无 Markdown）、极紧凑（≤150字），适合内联注入上下文窗口
const ChatInlineCompress = `将以下对话历史压缩为不超过150字的纯文本摘要，保留：安全事件名称/CVE编号、关键结论、未解决问题。省略寒暄和重复内容，不使用Markdown格式。

对话历史：
`
