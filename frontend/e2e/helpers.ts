import { execFileSync } from 'node:child_process'

export function manage(args: string[], input = '') {
  if (process.env.E2E_MANAGE_BIN) {
    execFileSync(process.env.E2E_MANAGE_BIN, args, {
      input,
      stdio: ['pipe', 'pipe', 'pipe'],
    })
  } else {
    execFileSync(
      'docker',
      ['compose', 'exec', '-T', 'api', 'manage', ...args],
      {
        cwd: '..',
        input,
        stdio: ['pipe', 'pipe', 'pipe'],
      },
    )
  }
}
