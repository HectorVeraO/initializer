import { BRAND } from '@/core/config/brand';
import { configureFeatureTest, render, textOf } from '@/core/testing/ui-testing';
import { Notfound } from './notfound';

describe('Notfound', () => {
  it('renders the brand above the 404 message', async () => {
    configureFeatureTest();
    const fixture = await render(Notfound);

    const root = fixture.nativeElement as HTMLElement;

    expect(textOf(root)).toContain(BRAND);
    expect(textOf(root)).toContain('404');
  });
});
