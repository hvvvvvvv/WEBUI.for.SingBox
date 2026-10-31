// End-to-end authentication checks against an isolated, fresh VM or preview.
import assert from 'node:assert/strict'
import { createHmac } from 'node:crypto'
import { setTimeout } from 'node:timers/promises'

const base = process.argv[2]
const mode = process.argv[3] ?? 'fresh'
assert.ok(['fresh', 'verify-persisted'].includes(mode), 'Unknown authentication smoke-test mode')
const password = 'OpenWrt-isolated-test-password!'
assert.match(base ?? '', /^http:\/\/127\.0\.0\.1:\d+$/,
  'Only isolated loopback test servers are allowed')
const call = async (path, args, token = '', success = true) => {
  const response = await fetch(`${base}/api/auth/${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: JSON.stringify({ args }), signal: AbortSignal.timeout(60000),
  })
  if (response.status === 401) throw Object.assign(new Error('Unauthorized'), { status: 401 })
  assert.equal(response.status, 200, `${path}: unexpected HTTP status`)
  assert.equal(response.headers.get('Cache-Control'), 'no-store')
  const result = await response.json()
  assert.equal(result.flag, success, `${path}: unexpected authentication outcome`)
  return result.data
}
const revoked = async (token) => {
  await assert.rejects(call('session', [], token), { status: 401 })
}
const decodeBase32 = (secret) => {
  let bits = 0, value = 0
  const bytes = []
  for (const char of secret.replace(/=+$/, '')) {
    const index = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(char)
    assert.ok(index >= 0)
    value = (value << 5) | index
    bits += 5
    if (bits >= 8) { bits -= 8; bytes.push((value >>> bits) & 255) }
  }
  return Buffer.from(bytes)
}
const totp = (secret, seconds) => {
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(seconds / 30)))
  const digest = createHmac('sha1', decodeBase32(secret)).update(counter).digest()
  return String((digest.readUInt32BE(digest[19] & 15) & 0x7fffffff) % 1000000).padStart(6, '0')
}
assert.equal(totp('GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ', 59), '287082')
if (mode === 'verify-persisted') {
  assert.deepEqual(JSON.parse(await call('status', [])), { enabled: true, totp_enabled: false },
    'Restart lost the authentication state established by the fresh smoke test')
  await call('login', [''], '', false)
  await call('login', ['incorrect'], '', false)
  const token = await call('login', [password])
  await call('session', [], token)
  console.log('Restart preserved the configured password and disabled TOTP state; empty and incorrect passwords remain rejected.')
} else {
  assert.deepEqual(JSON.parse(await call('status', [])), { enabled: false, totp_enabled: false },
    'VM must have fresh test credentials')
  const initial = await call('login', [''])
  const token = await call('setup', [password, '', ''], initial)
  await revoked(initial)
  await call('login', ['incorrect'], '', false)
  const enrollment = JSON.parse(await call('totp/setup', [password], token))
  assert.ok(enrollment.otpauth_uri.startsWith('otpauth://totp/'))
  const now = Date.now() / 1000
  if (now % 30 > 24) await setTimeout((31 - now % 30) * 1000)
  const code = totp(enrollment.secret, Date.now() / 1000)
  const enabled = JSON.parse(await call('totp/enable', [code], token))
  await revoked(token)
  assert.equal(enabled.recovery_codes.length, 10)
  assert.equal(JSON.parse(await call('status', [])).totp_enabled, true)
  await call('login', [password], '', false)
  await call('login', [password, code], '', false)
  const recovered = await call('login', [password, enabled.recovery_codes[0]])
  await call('login', [password, enabled.recovery_codes[0]], '', false)
  const replacement = await call('totp/disable', [password, enabled.recovery_codes[1]], recovered)
  await revoked(recovered)
  await revoked(enabled.token)
  assert.deepEqual(JSON.parse(await call('status', [])), { enabled: true, totp_enabled: false })
  await call('session', [], replacement)
  await call('login', [password])
  console.log('Password setup, TOTP enrollment, replay rejection, recovery consumption and session rotation passed.')
}
