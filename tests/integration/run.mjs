import { spawn } from 'node:child_process';
import { createWriteStream, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { StringDecoder } from 'node:string_decoder';

const arguments_ = new Set(process.argv.slice(2));
const phase = process.argv.find(value => value.startsWith('--phase='))?.slice('--phase='.length) || 'all';
const localPhases = new Set(['unit', 'race', 'coverage']);
if (!localPhases.has(phase) && !arguments_.has('--allow-privileged')) {
  throw new Error('This test fleet needs privileged Docker-in-Docker containers. Review compose.yaml, then pass --allow-privileged explicitly.');
}
const directory = dirname(fileURLToPath(import.meta.url));
const root = resolve(directory, '../..');
const artifacts = join(directory, 'artifacts');
const docker = process.env.DOCKER_BINARY || 'docker';
const compose = ['compose', '--project-name', 'lighthouse-verification', '-f', join(directory, 'compose.yaml')];
if (!['all', 'scans', 'fleet', 'recovery', ...localPhases].includes(phase)) throw new Error(`Unknown phase: ${phase}`);
mkdirSync(artifacts, { recursive: true });
const resultFile = join(artifacts, 'results.json');
const results = existsSync(resultFile) ? JSON.parse(readFileSync(resultFile, 'utf8')) : [];

async function run(name, command, args, environment = {}) {
  console.log(`\n[${name}] Starting`);
  const started = Date.now();
  const log = createWriteStream(join(artifacts, `${name}.log`));
  const child = spawn(command, args, { cwd: root, env: { ...process.env, ...environment }, stdio: ['ignore', 'pipe', 'pipe'] });
  const testCounts = { passed: 0, failed: 0, skipped: 0 };
  const decoder = new StringDecoder('utf8');
  let pending = '';
  child.stdout.on('data', chunk => {
    log.write(chunk);
    if (!args.includes('-json')) {
      process.stdout.write(chunk);
      return;
    }
    pending += decoder.write(chunk);
    const lines = pending.split('\n');
    pending = lines.pop();
    for (const line of lines) {
      if (!line.trim()) continue;
      const event = JSON.parse(line);
      if (event.Test && event.Action === 'pass') testCounts.passed++;
      if (event.Test && event.Action === 'fail') testCounts.failed++;
      if (event.Test && event.Action === 'skip') testCounts.skipped++;
      if (event.Action === 'fail' || event.Action === 'skip' || (event.Action === 'pass' && !event.Test)) console.log(`${event.Action}: ${event.Package} ${event.Test || ''}`);
      if (event.Action === 'output' && /Error Trace:|expected:|actual\s*:|--- FAIL/.test(event.Output)) process.stdout.write(event.Output);
    }
  });
  child.stderr.on('data', chunk => { process.stderr.write(chunk); log.write(chunk); });
  const code = await new Promise((accept, reject) => {
    child.once('error', reject);
    child.once('close', accept);
  });
  await new Promise(accept => log.end(accept));
  const previous = results.findIndex(result => result.name === name);
  const result = { name, exitCode: code, completedAt: new Date().toISOString(), durationMs: Date.now() - started, ...(args.includes('-json') ? { tests: testCounts } : {}) };
  writeFileSync(join(artifacts, `${name}.result.json`), JSON.stringify(result, null, 2));
  if (previous < 0) results.push(result);
  else results[previous] = result;
  writeFileSync(resultFile, JSON.stringify(results, null, 2));
  console.log(`[${name}] Exit ${code}`);
  if (args.includes('-json')) console.log(`[${name}] ${JSON.stringify(testCounts)}`);
  if (code !== 0) throw new Error(`${name} failed; see tests/integration/artifacts/${name}.log`);
  if (testCounts.skipped > 0) throw new Error(`${name} skipped ${testCounts.skipped} tests; inspect the recorded output before accepting this run.`);
}

const testCompile = ['run', '--rm', '-v', `${root}:/app`, '-v', 'go-mod-cache:/go/pkg/mod', '-v', 'go-build-cache:/root/.cache/go-build', '-v', 'lighthouse-verification-artifacts:/reports', '-w', '/app', '-e', 'GOTOOLCHAIN=local', '-e', 'CGO_ENABLED=0', '-e', 'GOMAXPROCS=1', '-e', 'GOGC=50', 'golang:1.26-alpine', 'go', 'test', '-c', '-tags', 'integration', '-o', '/reports/integration.test', './tests/integration'];
const testBase = ['run', '--rm', '--network', 'lighthouse-verification_fleet', '--memory', '192m', '-e', 'LIGHTHOUSE_INTEGRATION=1', '-e', 'GOMAXPROCS=1', '-e', 'GOMEMLIMIT=128MiB', '-v', 'lighthouse-verification-artifacts:/reports:ro', '-v', 'lighthouse-verification_proxy-data:/test-ca:ro', '--entrypoint', '/reports/integration.test'];
const runTest = (name, pattern, environment = []) => run(name, docker, [...testBase, ...environment, 'lighthouse:verification', `-test.run=${pattern}`, '-test.v=true', '-test.timeout=25m']);

try {
  if (phase === 'unit') {
    await run('go-unit', docker, ['run', '--rm', '-v', `${root}:/app`, '-v', 'go-mod-cache:/go/pkg/mod', '-v', 'go-build-cache:/root/.cache/go-build', '-v', `${artifacts}:/reports`, '-w', '/app', '-e', 'GOTOOLCHAIN=local', '-e', 'GOMAXPROCS=1', '-e', 'GOGC=50', 'golang:1.26-alpine', 'go', 'test', '-p', '1', '-json', '-covermode=atomic', '-coverprofile=/reports/coverage.out', './...', '-count=1']);
  } else if (phase === 'race') {
    await run('go-race', docker, ['run', '--rm', '-v', `${root}:/app`, '-v', 'go-mod-cache:/go/pkg/mod', '-v', 'go-build-cache:/root/.cache/go-build', '-w', '/app', '-e', 'GOTOOLCHAIN=local', '-e', 'GOMAXPROCS=2', '-e', 'GOGC=50', 'golang:1.26', 'go', 'test', '-race', '-p', '1', '-json', './...', '-count=1', '-timeout=10m']);
  } else if (phase === 'coverage') {
    await run('coverage-functions', docker, ['run', '--rm', '-v', `${root}:/app`, '-v', 'go-mod-cache:/go/pkg/mod', '-v', 'go-build-cache:/root/.cache/go-build', '-w', '/app', '-e', 'GOTOOLCHAIN=local', 'golang:1.26-alpine', 'go', 'tool', 'cover', '-func=tests/integration/artifacts/coverage.out']);
  } else {
    await run('compose-validation', docker, [...compose, 'config', '--quiet']);
    await run('compile-tests', docker, testCompile);
  }
  if (phase === 'all' || phase === 'fleet') {
    await run('build-image', docker, ['build', '--progress', 'plain', '-t', 'lighthouse:verification', root]);
    await run('start-fleet', docker, [...compose, 'up', '--detach', '--wait', '--wait-timeout', '240']);
    await runTest('fleet', '^TestFleet$');
  }
  if (phase === 'all' || phase === 'recovery') {
    try {
      await run('stop-spoke', docker, [...compose, 'stop', 'spoke-one']);
      await runTest('disconnected', '^TestDisconnectedFleet$', ['-e', 'LIGHTHOUSE_FAULT_PHASE=disconnected']);
    } finally {
      await run('restart-services', docker, [...compose, 'restart', 'hub', 'spoke-one']);
      await run('wait-recovery', docker, [...compose, 'up', '--detach', '--wait', '--wait-timeout', '180']);
    }
    await runTest('recovered', '^TestRecoveredFleet$', ['-e', 'LIGHTHOUSE_FAULT_PHASE=recovered']);
  }
  if (phase === 'all' || phase === 'scans') await runTest('scans', '^TestScanFleet$');
  if (phase === 'all') {
    await run('browser-tests', process.execPath, [join(root, 'frontend/node_modules/@playwright/test/cli.js'), 'test', '--config', join(root, 'frontend/playwright.config.js')], { LIGHTHOUSE_INTEGRATION: '1' });
  }
} catch (error) {
  process.exitCode = 1;
  console.error(error.message);
} finally {
  if (!localPhases.has(phase) && !arguments_.has('--keep')) {
    try {
      await run('cleanup', docker, [...compose, 'down', '--volumes', '--remove-orphans']);
    } catch (error) {
      process.exitCode = 1;
      console.error(error.message);
    }
  }
}