// The platform import rules of eslint.config.js must reject every import that couples a core-owned
// file to an optional module (or a module to the composition root): a core-only generated project
// does not ship the module folders and would stop compiling.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readdirSync, rmdirSync, rmSync } from 'node:fs';
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

// A product vertical exists only in a project that has one, so the platform side is probed by name: the
// regexes that reject it are built from the platform folders and hold in every project.
rejected.push(
  ['the core module imports a product vertical by alias', 'src/app/platform/core/probe/repro.ts', moduleImport('@/product/probe/module', 'probeModule'), 'Platform code never imports a product vertical'],
  ['the core module imports a product vertical by relative path', 'src/app/platform/core/probe/repro.ts', moduleImport('../../../product/probe/module', 'probeModule'), 'Platform code never imports a product vertical'],
  ['a module imports a product vertical by alias', `src/app/platform/${importerModule}/repro.ts`, moduleImport('@/product/probe/module', 'probeModule'), 'Platform code never imports a product vertical'],
  ['a module imports a product vertical by relative path', `src/app/platform/${importerModule}/repro.ts`, moduleImport('../../product/probe/module', 'probeModule'), 'Platform code never imports a product vertical'],
  ['the core module imports the project seed by alias', 'src/app/platform/core/probe/repro.ts', moduleImport('@/product.modules', 'productModules'), 'Platform code never imports a product vertical'],
  ['the core module imports the project seed by relative path', 'src/app/platform/core/probe/repro.ts', moduleImport('../../../product.modules', 'productModules'), 'Platform code never imports a product vertical'],
  ['a module imports the project seed by alias', `src/app/platform/${importerModule}/repro.ts`, moduleImport('@/product.modules', 'productModules'), 'Platform code never imports a product vertical'],
  ['the composition root imports a vertical module directly', 'src/app/platform/modules.ts', moduleImport('../product/probe/module', 'probeModule'), 'The composition root imports only'],
  ['the home page imports a core file deeply by alias', 'src/app/home/repro.ts', moduleImport('@/platform/core/access/access.port', 'ACCESS_PROVIDER'), 'Core-owned code imports a module only']
);

const allowed = [
  ['a module imports core through its barrel', `src/app/platform/${importerModule}/repro.ts`, moduleImport('@/platform/core', 'coreModule')],
  ['the composition root imports the project seed', 'src/app/platform/modules.ts', moduleImport('../product.modules', 'productModules')],
  ['the project seed imports the core barrel', 'src/app/product.modules.ts', moduleImport('./platform/core', 'coreModule')],
  ['the project seed registers a vertical', 'src/app/product.modules.ts', moduleImport('@/product/probe/module', 'probeModule')],
  ['a vertical spec imports the access fixture by relative path', 'src/app/product/probe/repro.spec.ts', moduleImport('../../../testing/fixtures/access-testing', 'accessContext')],
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

// A product vertical is a folder under src/app/product/, read at load time like the platform folders. Its
// block replaces the generic one wholesale, so a fresh process with a vertical of its own checks it.
test('a product vertical reaches the platform barrels and its own folder only', () => {
  const vertical = 'probe-vertical';
  const unknown = 'zz-unknown-vertical';
  const productDir = path.join(cwd, 'src/app/product');
  const probeDir = path.join(productDir, vertical);
  const createdProductDir = !existsSync(productDir);
  const at = (name, folder = vertical) => `src/app/product/${folder}/${name}`;
  const cases = [
    ['its own folder by alias', at('repro.ts'), moduleImport(`@/product/${vertical}/helpers`, 'helpers'), true],
    ['its own file relatively', at('repro.ts'), moduleImport('./helpers', 'helpers'), true],
    ['the shared test fixtures by relative path', at('shell/repro.spec.ts'), moduleImport('../../../../testing/fixtures/access-testing', 'accessContext'), true],
    ['the core barrel', at('repro.ts'), moduleImport('@/platform/core', 'coreModule'), true],
    ['the composition root', at('repro.ts'), moduleImport('@/platform/modules', 'installedModules'), true],
    ['a sibling vertical by alias', at('repro.ts'), moduleImport('@/product/zz-other/module', 'otherModule'), false],
    ['a sibling vertical by relative path', at('repro.ts'), moduleImport('../zz-other/module', 'otherModule'), false],
    ['a platform module folder', at('repro.ts'), moduleImport('@/platform/identity/module', 'identityModule'), false],
    ['a deep import into the core barrel', at('repro.ts'), moduleImport('@/platform/core/access/access.port', 'ACCESS_PROVIDER'), false],
    ['generic core code by alias', at('repro.ts'), moduleImport('@/core/api/api-error', 'apiError'), false],
    ['the shell by alias', at('repro.ts'), moduleImport('@/layout/component/app.layout', 'AppLayout'), false],
    ['the platform by escaping the folder', at('repro.ts'), moduleImport('../../platform/core', 'coreModule'), false],
    ['a known vertical from a folder the config never read', at('repro.ts', unknown), moduleImport(`@/product/${vertical}/helpers`, 'helpers'), false]
  ];
  const script = `
    import { ESLint } from 'eslint';
    const eslint = new ESLint({ cwd: process.argv[1] });
    const cases = JSON.parse(process.argv[2]);
    const out = [];
    for (const { file, code } of cases) {
      const [result] = await eslint.lintText(code, { filePath: process.argv[1] + '/' + file });
      out.push(result.messages.filter((message) => message.ruleId === 'no-restricted-imports').map((message) => message.message));
    }
    console.log(JSON.stringify(out));
  `;

  // The parent is created only when it is missing. The leaf is not recursive: an existing folder of that
  // name fails the test here instead of being deleted below.
  mkdirSync(productDir, { recursive: true });
  mkdirSync(probeDir);
  try {
    const payload = JSON.stringify(cases.map(([, file, code]) => ({ file, code })));
    const output = execFileSync(process.execPath, ['--input-type=module', '-e', script, cwd, payload], { cwd, encoding: 'utf8' });
    const results = JSON.parse(output);

    cases.forEach(([name, , , allowed], index) => {
      const messages = results[index];

      if (allowed) {
        assert.deepEqual(messages, [], `allowed: ${name} was rejected with ${JSON.stringify(messages)}`);

        return;
      }
      assert.ok(
        messages.some((message) => message.includes('A product vertical imports the platform through')),
        `rejected: ${name} was not rejected; got ${JSON.stringify(messages)}`
      );
    });
  } finally {
    rmSync(probeDir, { recursive: true, force: true });
    if (createdProductDir && readdirSync(productDir).length === 0) {
      rmdirSync(productDir);
    }
  }
});
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
