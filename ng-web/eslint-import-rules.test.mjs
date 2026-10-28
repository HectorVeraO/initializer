// The platform import rules of eslint.config.js must reject every import that couples a core-owned
// file to an optional module (or a module to the composition root): a core-only generated project
// does not ship the module folders and would stop compiling.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdirSync, readdirSync, rmSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';

const cwd = path.dirname(fileURLToPath(import.meta.url));
const eslint = new ESLint({ cwd });

/** Returns the no-restricted-imports messages ESLint reports for `code` at `relativePath`. */
async function restrictedImports(relativePath, code) {
  const [result] = await eslint.lintText(code, { filePath: path.join(cwd, relativePath) });
  const messages = result.messages.filter((message) => message.ruleId === 'no-restricted-imports');

  return messages;
}

const moduleImport = (specifier, name) => `import { ${name} } from '${specifier}';\nexport const x = ${name};\n`;

// Module folders found in the tree: none in a core-only generated project, so no module case is generated there.
const moduleFolders = readdirSync(path.join(cwd, 'src/app/platform'), { withFileTypes: true })
  .filter((entry) => entry.isDirectory() && entry.name !== 'core')
  .map((entry) => entry.name);

// Not a real folder: the module rule matches any folder under platform/ except core.
const importerModule = 'probe-importer';

const rejected = [
  ['the shell imports a core file deeply by alias', 'src/app/layout/repro.ts', moduleImport('@/platform/core/access/access.port', 'ACCESS_PROVIDER'), 'The skeleton imports platform modules only'],
  ['a module imports the composition root by relative path', `src/app/platform/${importerModule}/repro.ts`, moduleImport('../modules', 'installedModules'), 'A module never imports a sibling module folder'],
  ['another module imports core by relative path', `src/app/platform/${importerModule}/repro.ts`, moduleImport('../core/probe', 'probe'), 'A module never imports a sibling module folder'],
  ['the core module imports the composition root by relative path', 'src/app/platform/core/probe/repro.ts', moduleImport('../../modules', 'installedModules'), 'The core module never imports another'],
  ['a skeleton test fixture imports a module folder', 'src/testing/fixtures/repro.ts', moduleImport('@/platform/probe-module/session', 'sessionView'), 'Core-owned code imports a module only'],
  ['the topbar imports a module folder by alias', 'src/app/layout/component/topbar/repro.ts', moduleImport('@/platform/probe-module/module', 'moduleUnderTest'), 'The skeleton imports platform modules only'],
  ['tenancy imports identity by alias', 'src/app/platform/tenancy/repro.ts', moduleImport('@/platform/identity/session/session-store.service', 'SessionStore'), 'A module never imports another platform module']
];

// The relative-path rule is built from the folders found in the tree, so this case exists only where both folders do.
if (moduleFolders.includes('tenancy')) {
  rejected.push(['api-keys imports tenancy by relative path', 'src/app/platform/api-keys/items/repro.ts', moduleImport('../../tenancy/tenants/tenants-api.service', 'TenantsApi'), 'A module never imports a sibling module folder']);
}

for (const name of moduleFolders) {
  const specifier = `@/platform/${name}/module`;

  rejected.push(
    [`the home page (core-owned) imports ${name} by alias`, 'src/app/home/repro.ts', moduleImport(specifier, 'moduleUnderTest'), 'Core-owned code imports a module only'],
    [`generic core code imports ${name} by alias`, 'src/app/core/repro.ts', moduleImport(specifier, 'moduleUnderTest'), 'Core-owned code imports a module only'],
    [`the bootstrap file imports ${name} by alias`, 'src/main.repro.ts', moduleImport(specifier, 'moduleUnderTest'), 'Core-owned code imports a module only'],
    [`the core module imports ${name} by alias`, 'src/app/platform/core/probe/repro.ts', moduleImport(specifier, 'moduleUnderTest'), 'The core module never imports another'],
    [`the core module imports ${name} by relative path`, 'src/app/platform/core/probe/repro.ts', moduleImport(`../../${name}/module`, 'moduleUnderTest'), 'The core module never imports another'],
    [`the core module imports ${name} by a deep relative path`, 'src/app/platform/core/probe/repro.ts', moduleImport(`../../../platform/${name}/module`, 'moduleUnderTest'), 'The core module never imports another'],
    [`the shell imports ${name} by relative path`, 'src/app/layout/component/repro.ts', moduleImport(`../../platform/${name}/module`, 'moduleUnderTest'), 'The skeleton imports platform modules only'],
    [`another module imports ${name} by relative path`, `src/app/platform/${importerModule}/repro.ts`, moduleImport(`../${name}/module`, 'moduleUnderTest'), 'A module never imports a sibling module folder'],
    [`another module imports ${name} by a deep relative path`, `src/app/platform/${importerModule}/repro.ts`, moduleImport(`../../platform/${name}/module`, 'moduleUnderTest'), 'A module never imports a sibling module folder']
  );
}

const allowed = [
  ['a module imports core through its barrel', `src/app/platform/${importerModule}/repro.ts`, moduleImport('@/platform/core', 'coreModule')],
  ['the shell imports the composition root by alias', 'src/app/layout/repro.ts', moduleImport('@/platform/modules', 'installedModules')],
  ['the shell imports the core barrel by alias', 'src/app/layout/repro.ts', moduleImport('@/platform/core', 'coreModule')],
  ['the home page imports the composition root by alias', 'src/app/home/repro.ts', moduleImport('@/platform/modules', 'installedModules')],
  ['the bootstrap file imports the core barrel', 'src/main.repro.ts', moduleImport('@/platform/core', 'coreModule')],
  [
    'a module imports the access, tenant directory and tenant selection ports from the core barrel',
    `src/app/platform/${importerModule}/repro.ts`,
    "import { ACCESS_PROVIDER, TENANT_DIRECTORY, TENANT_SELECTION } from '@/platform/core';\nexport const x = [ACCESS_PROVIDER, TENANT_DIRECTORY, TENANT_SELECTION];\n"
  ],
  ['a module spec imports the access fixture by relative path', `src/app/platform/${importerModule}/shell/repro.spec.ts`, moduleImport('../../../../testing/fixtures/access-testing', 'accessContext')]
];

for (const [name, file, code, expected] of rejected) {
  test(`rejects: ${name}`, async () => {
    const messages = await restrictedImports(file, code);

    assert.ok(
      messages.some((message) => message.message.includes(expected)),
      `${file} was not rejected by the expected rule (${expected}); got: ${JSON.stringify(messages.map((message) => message.message))}`
    );
  });
}

for (const [name, file, code] of allowed) {
  test(`allows: ${name}`, async () => {
    const messages = await restrictedImports(file, code);

    assert.deepEqual(
      messages.map((message) => message.message),
      []
    );
  });
}

// A module folder created after the config was first loaded must be covered too: the config reads the tree
// at load time, so a fresh process lints against a tree that holds a folder no list ever named.
test('rejects: a module folder that exists only for this run', () => {
  const probeFolder = 'zz-probe-module';
  const probeDir = path.join(cwd, 'src/app/platform', probeFolder);
  const script = `
    import { ESLint } from 'eslint';
    const eslint = new ESLint({ cwd: process.argv[1] });
    const code = "import { m } from '../../${probeFolder}/module'; export const x = m;";
    const [result] = await eslint.lintText(code, { filePath: process.argv[1] + '/src/app/platform/core/probe/repro.ts' });
    console.log(JSON.stringify(result.messages.filter((message) => message.ruleId === 'no-restricted-imports').map((message) => message.message)));
  `;

  // Not recursive: an existing folder of that name fails the test here instead of being deleted below.
  mkdirSync(probeDir);
  try {
    const output = execFileSync(process.execPath, ['--input-type=module', '-e', script, cwd], { cwd, encoding: 'utf8' });
    const messages = JSON.parse(output);

    assert.ok(
      messages.some((message) => message.includes('The core module never imports another')),
      `a new module folder was not rejected; got: ${output}`
    );
  } finally {
    rmSync(probeDir, { recursive: true, force: true });
  }
});
