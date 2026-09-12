import { expect, test } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { mkdir, writeFile } from 'node:fs/promises'

// Real host acceptance only. No route interception, seeded Run, or provider double.
const authPath = process.env.SENTINELOPS_LIVE_AUTH_FILE
const apiURL = process.env.SENTINELOPS_LIVE_API_URL ?? 'http://127.0.0.1:8001'
test.describe('real persisted Runtime', () => {
  test.skip(!authPath, 'NOT RUN: set SENTINELOPS_LIVE_AUTH_FILE after real host admin login')
  test('v2 create, worker terminal, incremental tail and all read surfaces', async ({ page, request }, testInfo) => {
    const auth = JSON.parse(readFileSync(authPath!, 'utf8'))
    const headers = { Authorization: `Bearer ${auth.token}` }
    const session = `phase-f-browser-${crypto.randomUUID()}`
    const created = await request.post(`${apiURL}/api/chat/v2/runs`, { headers, data: { session_id: session, query: '请制定简短计划，查询最近的安全事件并给出只读摘要。不要执行任何写操作或通知。' } })
    expect(created.ok()).toBe(true)
    const { data: run } = await created.json()
    expect(run.run_id).toBeTruthy()
    await page.addInitScript(value => {
      localStorage.setItem('token', value.token)
      localStorage.setItem('auth-storage', JSON.stringify({ state: { ...value, userID: value.user_id }, version: 0 }))
    }, auth)
    const errors: string[] = []
    page.on('pageerror', e => errors.push(e.message))
    await page.goto(`/runtime/runs/${run.run_id}`)
    let detail: any
    await expect.poll(async () => {
      const response = await request.get(`${apiURL}/api/runtime/v1/runs/${run.run_id}`, { headers })
      expect(response.ok()).toBe(true)
      detail = (await response.json()).data
      return detail.item.summary.status
    }, { timeout: 90000, intervals: [1000] }).toMatch(/^(succeeded|failed|parked|canceled)$/)
    const resources: Record<string, unknown> = {}
    for (const resource of ['attempts', 'timeline', 'checkpoints', 'approvals', 'effects', 'evidence', 'context', 'traces']) {
      const response = await request.get(`${apiURL}/api/runtime/v1/runs/${run.run_id}/${resource}`, { headers })
      expect(response.ok()).toBe(true)
      resources[resource] = (await response.json()).data
    }
    const attempts = (resources.attempts as any).items
    expect(attempts.length).toBeGreaterThan(0)
    expect(attempts[0].executing_worker_fingerprint).toBeTruthy()
    const events = await request.get(`${apiURL}/api/chat/v2/runs/${run.run_id}/events?after_seq=0`, { headers })
    expect(events.ok()).toBe(true)
    const body = await events.text()
    const seqs = [...body.matchAll(/^id:\s*(\d+)/gm)].map(m => Number(m[1]))
    expect(seqs.length).toBeGreaterThan(0)
    expect(new Set(seqs).size).toBe(seqs.length)
    const lastSeq = Math.max(...seqs)
    const resumed = await request.get(`${apiURL}/api/chat/v2/runs/${run.run_id}/events?after_seq=${lastSeq}`, { headers })
    expect(await resumed.text()).not.toMatch(/^id:/m)
    const list = await request.get(`${apiURL}/api/runtime/v1/runs?session_id=${session}`, { headers })
    expect((await list.json()).data.items.map((item: any) => item.run_id)).toEqual([run.run_id])
    const afterEffects = await request.get(`${apiURL}/api/runtime/v1/runs/${run.run_id}/effects`, { headers })
    expect((await afterEffects.json()).data.items).toEqual((resources.effects as any).items)
    const dir = `../output/playwright/runtime-${testInfo.project.use.viewport!.width}/live`
    await mkdir(dir, { recursive: true })
    for (const tab of ['overview', 'timeline', 'attempts', 'effects', 'evidence', 'context', 'trace']) {
      await page.goto(`/runtime/runs/${run.run_id}?tab=${tab}`)
      await expect(page.getByRole('tabpanel')).toBeVisible()
      await expect(page.getByRole('heading', { name: `Run ${run.run_id}`, exact: true })).toBeVisible()
      await page.waitForLoadState('networkidle')
      await page.screenshot({ path: `${dir}/${tab}.png`, fullPage: true })
      expect(await page.locator('main').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    }
    for (const path of ['capabilities', 'safety', 'worker-health']) {
      await page.goto(`/runtime/${path}`)
      await expect(page.locator('main h1')).toBeVisible()
      await page.waitForLoadState('networkidle')
      await page.screenshot({ path: `${dir}/${path}.png`, fullPage: true })
      expect(await page.locator('main').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    }
    expect(errors).toEqual([])
    await writeFile(`${dir}/facts.json`, JSON.stringify({ run_id: run.run_id, session_id: session, status: detail.item.summary.status, last_seq: lastSeq, detail, resources, page_errors: errors, overflow: false }, null, 2))
  })

  test('admin cancel returns 202 and reaches durable operation terminal', async ({ request }, testInfo) => {
    const auth = JSON.parse(readFileSync(authPath!, 'utf8'))
    const headers = { Authorization: `Bearer ${auth.token}` }
    const response = await request.post(`${apiURL}/api/chat/v2/runs`, { headers, data: { session_id: `phase-f-cancel-${crypto.randomUUID()}`, query: '制定只读安全事件分析计划。' } })
    expect(response.ok()).toBe(true)
    const runId = (await response.json()).data.run_id
    const detail = (await (await request.get(`${apiURL}/api/runtime/v1/runs/${runId}`, { headers })).json()).data.item
    const body = { action: 'cancel', reason: 'Phase F host acceptance: cancel before further processing', expected_generation: detail.summary.lease_generation, idempotency_key: crypto.randomUUID() }
    const accepted = await request.post(`${apiURL}/api/runtime/v1/runs/${runId}/recovery`, { headers, data: body })
    expect(accepted.status()).toBe(202)
    const op = (await accepted.json()).data.operation
    const repeat = await request.post(`${apiURL}/api/runtime/v1/runs/${runId}/recovery`, { headers, data: body })
    expect((await repeat.json()).data.operation.operation_id).toBe(op.operation_id)
    let operation: any
    await expect.poll(async () => {
      operation = (await (await request.get(`${apiURL}/api/runtime/v1/operations/${op.operation_id}`, { headers })).json()).data.item
      return operation.terminal
    }, { timeout: 45000 }).toBe(true)
    expect(operation.status).toBe('canceled')
    const final = (await (await request.get(`${apiURL}/api/runtime/v1/runs/${runId}`, { headers })).json()).data.item
    expect(final.summary.status).toBe('canceled')
    const dir = `../output/playwright/runtime-${testInfo.project.use.viewport!.width}/live`
    await mkdir(dir, { recursive: true })
    await writeFile(`${dir}/recovery.json`, JSON.stringify({ run_id: runId, operation, final_status: final.summary.status, idempotent_repeat: true }, null, 2))
  })
})
