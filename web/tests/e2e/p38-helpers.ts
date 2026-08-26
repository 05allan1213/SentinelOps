import { expect, type APIRequestContext, type Page } from '@playwright/test'
import crypto from 'node:crypto'

export interface AuthIdentity {
  token: string
  userID: string
  role: string
  username: string
}

export interface DurableRun {
  run_id: string
  session_id: string
  status: string
}

export interface ApprovalSnapshot {
  id: string
  run_id: string
  tool_name: string
  proposal_hash: string
  status: string
  version: number
}

export interface UnknownEffectSnapshot {
  id: string
  run_id: string
  tool_name: string
  effect_step: string
  effect_type: string
  status: string
  version: number
  reconciliation_attempts: number
}

export interface SSEEvent {
  id: number
  type: string
  data: unknown
}

const adminPassword = process.env.SENTINELOPS_E2E_ADMIN_PASSWORD ?? 'sentinelops-e2e-admin-password'
const requesterPassword = 'p38-requester-password'

export function signIdentityToken(userID: string, username: string, role: string): string {
  const header = Buffer.from(JSON.stringify({ alg: 'HS256', typ: 'JWT' })).toString('base64url')
  const payload = Buffer.from(JSON.stringify({
    uid: userID,
    username,
    role,
    exp: Math.floor(Date.now() / 1000) + 3600,
  })).toString('base64url')
  const input = `${header}.${payload}`
  const signature = crypto
    .createHmac('sha256', process.env.SENTINELOPS_E2E_JWT_SECRET ?? 'sentinelops-e2e-jwt-secret')
    .update(input)
    .digest('base64url')
  return `${input}.${signature}`
}

export function signViewerToken(): string {
  return signIdentityToken('p38-viewer', 'p38-viewer', 'viewer')
}

async function installIdentity(page: Page, identity: AuthIdentity): Promise<void> {
  await page.goto('/login')
  await page.evaluate((auth) => {
    localStorage.setItem('token', auth.token)
    localStorage.setItem('auth-storage', JSON.stringify({ state: auth, version: 0 }))
  }, identity)
}

export async function loginAdmin(page: Page): Promise<AuthIdentity> {
  const response = await page.request.post('/api/auth/v1/login', {
    data: { username: 'admin', password: adminPassword },
  })
  expect(response.ok()).toBeTruthy()
  const body = await response.json() as { data?: { token?: string; user_id?: string; username?: string; role?: string } }
  const data = body.data ?? {}
  const identity: AuthIdentity = {
    token: data.token ?? '',
    userID: data.user_id ?? '',
    role: data.role ?? 'admin',
    username: data.username ?? 'admin',
  }
  expect(identity.token).toBeTruthy()
  await installIdentity(page, identity)
  return identity
}

export async function loginRequester(page: Page, adminToken: string): Promise<string> {
  const requesterUsername = `p38-requester-${crypto.randomUUID().slice(0, 8)}`
  const register = await page.request.post('/api/auth/v1/register', {
    headers: { Authorization: `Bearer ${adminToken}` },
    data: { username: requesterUsername, password: requesterPassword },
  })
  if (register.ok()) {
    const body = await register.json() as { data?: { user_id?: string; username?: string } }
    const data = body.data
    if (data?.user_id && data.username) {
      return signIdentityToken(data.user_id, data.username, 'operator')
    }
  }
  const response = await page.request.post('/api/auth/v1/login', {
    data: { username: requesterUsername, password: requesterPassword },
  })
  expect(response.ok()).toBeTruthy()
  const body = await response.json() as { data?: { user_id?: string; username?: string } }
  const data = body.data ?? {}
  expect(data.user_id).toBeTruthy()
  expect(data.username).toBeTruthy()
  return signIdentityToken(data.user_id as string, data.username as string, 'operator')
}

export async function createDurableRun(page: Page, token: string, query: string): Promise<DurableRun> {
  const response = await page.request.post('/api/chat/v2/runs', {
    headers: { Authorization: `Bearer ${token}` },
    data: { session_id: `p38-${crypto.randomUUID()}`, query, agent: 'plan_agent' },
  })
  expect(response.ok()).toBeTruthy()
  const body = await response.json() as { data?: DurableRun }
  expect(body.data?.run_id).toBeTruthy()
  return body.data as DurableRun
}

export async function waitForPendingApproval(
  request: APIRequestContext,
  token: string,
  runID: string,
): Promise<ApprovalSnapshot> {
  let found: ApprovalSnapshot | undefined
  await expect.poll(async () => {
    const response = await request.get('/api/ops/v1/approvals?limit=100', {
      headers: { Authorization: `Bearer ${token}` },
    })
    if (!response.ok()) return false
    const body = await response.json() as { data?: { items?: ApprovalSnapshot[] } }
    found = body.data?.items?.find(item => item.run_id === runID && item.status === 'pending')
    return Boolean(found)
  }, { timeout: 90_000, intervals: [250, 500, 1000, 2000] }).toBe(true)
  return found as ApprovalSnapshot
}

export async function waitForUnknownEffect(
  request: APIRequestContext,
  token: string,
  runID: string,
): Promise<UnknownEffectSnapshot> {
  let found: UnknownEffectSnapshot | undefined
  await expect.poll(async () => {
    const response = await request.get('/api/ops/v1/effects/unknown?limit=1000', {
      headers: { Authorization: `Bearer ${token}` },
    })
    if (!response.ok()) return false
    const body = await response.json() as { data?: { items?: UnknownEffectSnapshot[] } }
    found = body.data?.items?.find(item => item.run_id === runID && item.status === 'unknown')
    return Boolean(found)
  }, { timeout: 90_000, intervals: [250, 500, 1000, 2000] }).toBe(true)
  return found as UnknownEffectSnapshot
}

export async function readSSESegment(
  request: APIRequestContext,
  token: string,
  runID: string,
  afterSeq: number,
): Promise<SSEEvent[]> {
  const response = await request.get(`/api/chat/v2/runs/${runID}/events?after_seq=${afterSeq}`, {
    headers: { Authorization: `Bearer ${token}` },
    timeout: 120_000,
  })
  expect(response.ok()).toBeTruthy()
  const text = await response.text()
  const events: SSEEvent[] = []
  for (const block of text.split(/\n\n+/)) {
    const lines = block.split('\n')
    const type = lines.find(line => line.startsWith('event: '))?.slice('event: '.length)
    const idText = lines.find(line => line.startsWith('id: '))?.slice('id: '.length)
    const dataLines = lines.filter(line => line.startsWith('data: ')).map(line => line.slice('data: '.length))
    if (!type || !idText || dataLines.length === 0 || dataLines.join('\n') === '[DONE]') continue
    const raw = dataLines.join('\n')
    let data: unknown = raw
    try {
      data = JSON.parse(raw) as unknown
    } catch {
      // Keep non-JSON diagnostic payloads observable without failing parsing.
    }
    events.push({ id: Number(idText), type, data })
  }
  return events
}

export async function readSSEUntilTerminal(
  request: APIRequestContext,
  token: string,
  runID: string,
  afterSeq = 0,
): Promise<SSEEvent[]> {
  const all: SSEEvent[] = []
  let cursor = afterSeq
  for (let attempt = 0; attempt < 32; attempt += 1) {
    const segment = await readSSESegment(request, token, runID, cursor)
    all.push(...segment)
    if (segment.length === 0) break
    if (segment.some(event => event.type === 'run.completed' || event.type === 'run.failed')) return all
    const next = segment[segment.length - 1].id
    if (!Number.isFinite(next) || next <= cursor) break
    cursor = next
  }
  return all
}

export async function acceptUnknownWithRetry(
  request: APIRequestContext,
  token: string,
  effectID: string,
  runID: string,
): Promise<void> {
  await expect.poll(async () => {
    const response = await request.post(`/api/ops/v1/effects/${effectID}/accept-unknown`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        run_id: runID,
        reason: 'P38 admin accepts provider unknown window',
        evidence: { source: 'provider-double', decision: 'accepted_unknown' },
      },
    })
    if (response.ok()) return true
    if (response.status() === 409) return false
    throw new Error(`accept-unknown failed with HTTP ${response.status()}`)
  }, { timeout: 90_000, intervals: [250, 500, 1000, 2000] }).toBe(true)
}

export function eventAttributes(event: SSEEvent): Record<string, unknown> {
  if (!event.data || typeof event.data !== 'object') return {}
  const envelope = event.data as { data?: unknown }
  if (!envelope.data || typeof envelope.data !== 'object') return {}
  return envelope.data as Record<string, unknown>
}
