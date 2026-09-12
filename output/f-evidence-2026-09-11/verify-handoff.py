"""Verify the frozen Phase F handoff against source and recorded executions."""
from pathlib import Path
import json
import re
import subprocess

root = Path(__file__).resolve().parents[2]
evidence = Path(__file__).resolve().parent
matrix = root / 'docs/implementation/runtime-requirement-evidence-2026-08-31.md'
rows = [[cell.strip() for cell in line.split('|')[1:-1]] for line in matrix.read_text().splitlines() if re.match(r'^\| [RF]\d\d \|', line)]
expected = {f'R{i:02}' for i in range(1,33)} | {f'F{i:02}' for i in range(1,22)}
assert len(rows) == 53 and {row[0] for row in rows} == expected
for row in rows:
    assert len(row) == 6, row
    assert row[1] and row[2] and row[3]
    status = 'NEEDS PRODUCT DECISION' if row[0] == 'F14' else 'OUT OF SCOPE' if row[0] == 'F20' else 'PASS'
    assert row[4] == status, row
    assert (root / row[3].strip('`')).is_file(), row
    subprocess.run(['git','cat-file','-e',row[5] + '^{commit}'],cwd=root,check=True)
terms = [bytes.fromhex(value).decode() for value in ['544f444f','544244','6c61746572','617070726f707269617465','6173206e6565646564','73696d696c617220746f','72656c61746564207465737473','68616e646c65206572726f7273','706f6c697368205549']]
for path in [root/'output/final-implementation-plan-2026-08-31.md', matrix]:
    content = path.read_text().lower()
    assert not any(term.lower() in content for term in terms), path
frontend = (root/'web/src/types/runtime.ts').read_text()
enums = {
 'RuntimeStatus': {'pending','running','waiting_approval','retryable_failed','parked','reconciling','succeeded','failed','canceled'},
 'CurrentPhase': {'planning','executing','waiting_approval','recovering','reconciling','completed','failed','unknown'},
 'OperationStatus': {'accepted','running','succeeded','failed','canceled','rejected'},
 'Availability': {'available','partial','unavailable'},
 'DataQuality': {'complete','reconstructed','partial','unknown'},
}
backend = '\n'.join(path.read_text() for path in (root/'api/runtime/v1').glob('*.go') if not path.name.endswith('_test.go'))
for name, values in enums.items():
    part = re.search(r'export type '+name+r'\s*=([\s\S]*?)(?=export |$)', frontend).group(1)
    assert set(re.findall(r"'([^']+)'",part)) == values, name
    declared = re.findall(r'\w+\s+'+name+r'\s*=\s*(?:'+name+r'\(workflow\.(\w+)\)|"([^"\n]+)")', backend)
    actual = set()
    workflow_source = '\n'.join(path.read_text() for path in (root/'internal/ai/workflow').glob('*.go') if not path.name.endswith('_test.go'))
    for alias, literal in declared:
        if alias:
            actual.add(re.search(r'\b'+alias+r'\s*=\s*"([^"\n]+)"', workflow_source).group(1))
        else:
            actual.add(literal)
    assert actual == values, (name, actual, values)
assert "baseURL: '/api'" in (root/'web/src/services/api.ts').read_text()
assert "import api from './api'" in (root/'web/src/services/runtime.ts').read_text()
assert (root/'web/src/main.tsx').read_text().count('new QueryClient(') == 1
assert 'request.Response.WriteHeader(status)' in (root/'internal/controller/runtime/runtime.go').read_text()
assert not subprocess.check_output(['git','diff','a1e80e7','HEAD','--','utility/middleware/response.go','migrations','manifest','web/src/pages/logs'],cwd=root,text=True)
assert not (root/'docs/implementation/logs-route-decision-2026-08-31.md').exists()
assert '154 passed' in (evidence/'f09-browser-suite.log').read_text()
assert '10 passed' in (evidence/'f09-live-pass.log').read_text()
assert '6 passed' in (evidence/'final-smoke.log').read_text()
assert '289 passed' in (evidence/'final-unit.log').read_text()
assert '0 errors' in (evidence/'final-lint.log').read_text()
log = (evidence/'backend-final-utc.log').read_text()
assert not re.search(r'^FAIL|^--- FAIL',log,re.M)
for package in ['api/runtime/v1','internal/service/runtime','internal/controller/runtime','internal/dao/mysql','internal/ai/workflow','internal/ai/runtime']:
    assert re.search(r'^ok\s+SentinelOps/'+package+r'\s',log,re.M), package
for width in [1280,1440]:
    directory = root/f'output/playwright/runtime-{width}'
    facts = json.loads((directory/'live/facts.json').read_text())
    recovery = json.loads((directory/'live/recovery.json').read_text())
    assert facts['status'] == 'succeeded' and facts['overflow'] is False and facts['page_errors'] == []
    assert facts['detail']['item']['compatibility']['worker_match']
    assert recovery['operation']['terminal'] and recovery['final_status'] == 'canceled'
    assert recovery['idempotent_repeat']
    for name in ['overview','timeline','attempts','effects','evidence','context','trace','capabilities','safety','worker-health']:
        assert (directory/'live'/f'{name}.png').stat().st_size > 0
    for name in ['loading','empty','unavailable','failed','parked','reconciling','succeeded']:
        assert (directory/'final'/f'{name}.png').stat().st_size > 0
audit = json.loads((evidence/'live-console-audit.json').read_text())
assert len(audit) == 22
assert all(not row['console_errors'] and row['dimensions']['page'] == row['dimensions']['viewport'] and row['dimensions']['main'] <= row['dimensions']['mainWidth'] for row in audit)
subprocess.run(['git','diff','--check'],cwd=root,check=True)
print('PASS: 53 unique requirements; source/commit references; enums/client/scope; all required logs; real desktop facts and screenshots; whitespace')
