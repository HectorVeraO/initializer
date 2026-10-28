import { test } from '@playwright/test';

test('alpha alone', { tag: ['@alpha'] }, async () => {});

test('alpha with beta', { tag: ['@alpha', '@beta'] }, async () => {});
