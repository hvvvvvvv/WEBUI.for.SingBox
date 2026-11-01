#!/usr/bin/env node
// Maintenance only: explicit reviewed commit + expected hashes, never latest.
import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { createHash, randomUUID } from 'node:crypto'
import dns from 'node:dns'
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { promisify } from 'node:util'

const run = promisify(execFile)
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const destination = path.join(root, 'bridge/config/ruleset_seed')
const parent = path.dirname(destination)
const allowed = [
  ['Category-Ads', 'geosite-category-ads-all.srs', 'geo/geosite/category-ads-all.srs'],
  ['GeoIP-Private', 'geoip-private.srs', 'geo/geoip/private.srs'],
  ['GeoSite-Private', 'geosite-private.srs', 'geo/geosite/private.srs'],
  ['GeoIP-CN', 'geoip-cn.srs', 'geo/geoip/cn.srs'],
  ['GeoSite-CN', 'geosite-cn.srs', 'geo/geosite/cn.srs'],
  ['GeoLocation-!CN', 'geosite-geolocation-not-cn.srs', 'geo/geosite/geolocation-!cn.srs'],
]
const options = {}
for (let index = 2; index < process.argv.length; index++) {
  const option = process.argv[index]
  if (option === '--verify-only') options.verify = true
  else if (['--commit', '--core', '--manifest'].includes(option)) {
    assert.ok(process.argv[index + 1] && !process.argv[index + 1].startsWith('--'), `${option} needs a value`)
    options[option.slice(2)] = process.argv[++index]
  } else throw new Error(`Unknown option: ${option}`)
}
assert.match(options.commit || '', /^[a-f0-9]{40}$/, '--commit must be an explicit 40-character commit SHA')
assert.ok(options.core, '--core must name a sing-box executable')
const core = path.resolve(options.core)
const manifest = JSON.parse(await fs.readFile(path.resolve(options.manifest || path.join(destination, 'manifest.json')), 'utf8'))
assert.equal(manifest.schema_version, 1)
assert.equal(manifest.snapshot, options.commit, 'manifest must pin the requested commit')
assert.equal(manifest.repository, 'https://github.com/MetaCubeX/meta-rules-dat')
assert.match(manifest.verified_core, /^\d+\.\d+\.\d+$/)
assert.equal(manifest.files.length, allowed.length, 'all six default rule sets are required')
for (let index = 0; index < allowed.length; index++) {
  const entry = manifest.files[index]
  assert.deepEqual([entry.tag, entry.file, entry.path], allowed[index], 'unexpected rule-set tag or path')
  assert.equal(entry.source_file, `source/${entry.file.replace(/\.srs$/, '.json')}`)
  for (const field of ['sha256', 'source_sha256']) assert.match(entry[field], /^[a-f0-9]{64}$/)
  for (const field of ['bytes', 'source_bytes']) assert.ok(Number.isSafeInteger(entry[field]) && entry[field] > 0 && entry[field] <= 16 * 1024 * 1024)
}
const executeCore = args => run(core, args, { windowsHide: true, timeout: 30000, maxBuffer: 1024 * 1024 })
const version = await executeCore(['version'])
assert.equal(version.stdout.match(/^sing-box version (\S+)/m)?.[1], manifest.verified_core, 'use the core version recorded in the manifest')
const digest = data => createHash('sha256').update(data).digest('hex')
const checkedData = (content, size, hash, name) => {
  assert.equal(content.length, size, `${name}: unexpected length`)
  assert.equal(digest(content), hash, `${name}: SHA-256 mismatch`)
  return content
}
dns.setDefaultResultOrder('ipv4first')
const download = async (relativePath, expectedSize, expectedHash) => {
  const url = `https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/${options.commit}/${relativePath}`
  const response = await fetch(url, { signal: AbortSignal.timeout(45000), redirect: 'error' })
  assert.equal(response.status, 200, `${relativePath}: HTTP ${response.status}`)
  const parts = []
  let size = 0
  for await (const chunk of response.body) {
    size += chunk.length
    assert.ok(size <= expectedSize, `${relativePath}: oversized response`)
    parts.push(chunk)
  }
  return checkedData(Buffer.concat(parts), expectedSize, expectedHash, relativePath)
}
// All removable/movable paths are fixed sibling directories under bridge/config.
const assertTemporary = directory => {
  assert.equal(path.dirname(path.resolve(directory)), parent)
  assert.match(path.basename(directory), /^\.ruleset-seed-(?:stage|backup)-[a-zA-Z0-9-]+$/)
}
const stage = await fs.mkdtemp(path.join(parent, '.ruleset-seed-stage-'))
const backup = path.join(parent, `.ruleset-seed-backup-${randomUUID()}`)
assertTemporary(stage)
assertTemporary(backup)
let savedPrevious = false
let installed = false
try {
  await fs.cp(destination, stage, { recursive: true, errorOnExist: false })
  for (const entry of manifest.files) {
    const sourcePath = entry.path.replace(/\.srs$/, '.json')
    const binary = options.verify
      ? checkedData(await fs.readFile(path.join(destination, entry.file)), entry.bytes, entry.sha256, entry.file)
      : await download(entry.path, entry.bytes, entry.sha256)
    const source = options.verify
      ? checkedData(await fs.readFile(path.join(destination, entry.source_file)), entry.source_bytes, entry.source_sha256, entry.source_file)
      : await download(sourcePath, entry.source_bytes, entry.source_sha256)
    assert.equal(JSON.parse(source).version, manifest.srs_version)
    await fs.writeFile(path.join(stage, entry.file), binary)
    await fs.writeFile(path.join(stage, entry.source_file), source)
    const decoded = path.join(stage, 'decoded.json')
    const rebuilt = path.join(stage, 'rebuilt.srs')
    await executeCore(['rule-set', 'decompile', path.join(stage, entry.file), '--output', decoded])
    await executeCore(['rule-set', 'compile', path.join(stage, entry.source_file), '--output', rebuilt])
    checkedData(await fs.readFile(rebuilt), entry.bytes, entry.sha256, `${entry.file} rebuilt from source`)
    await fs.unlink(decoded)
    await fs.unlink(rebuilt)
  }
  await fs.writeFile(path.join(stage, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n')
  await fs.writeFile(path.join(stage, 'SHA256SUMS'), manifest.files.flatMap(entry => [`${entry.sha256}  ${entry.file}`, `${entry.source_sha256}  ${entry.source_file}`]).join('\n') + '\n')
  if (!options.verify) {
    // The two renames stay on one filesystem. Ordinary replacement failures
    // restore the old directory; an interrupted run retains its backup for recovery.
    await fs.rename(destination, backup)
    savedPrevious = true
    await fs.rename(stage, destination)
    installed = true
    await fs.rm(backup, { recursive: true })
    savedPrevious = false
  }
  console.log(`${options.verify ? 'Verified' : 'Updated'} ${manifest.files.length} rule sets at ${manifest.snapshot} with sing-box ${manifest.verified_core}`)
} catch (error) {
  if (savedPrevious && !installed) {
    try { await fs.rename(backup, destination); savedPrevious = false }
    catch (restoreError) { console.error(`Previous resources retained at ${backup}: ${restoreError.message}`) }
  }
  throw error
} finally {
  await fs.rm(stage, { recursive: true, force: true })
}
