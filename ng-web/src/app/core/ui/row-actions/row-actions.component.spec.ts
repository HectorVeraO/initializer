import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { configureFeatureTest, settle, stubMatchMedia } from '@/core/testing/ui-testing';
import { RowAction, RowActionsComponent } from './row-actions.component';

@Component({
  selector: 'p-row-actions-host',
  standalone: true,
  imports: [RowActionsComponent],
  template: '<p-row-actions [actions]="actions" />'
})
class HostComponent {
  readonly calls: string[] = [];
  actions: RowAction[] = [];

  readonly edit: RowAction = { id: 'edit', label: 'Editar', icon: 'pi pi-pencil', testId: 'row-edit', run: () => this.calls.push('edit') };
  readonly review: RowAction = { id: 'review', label: 'Revisar', icon: 'pi pi-check-square', testId: 'row-review', disabled: true, run: () => this.calls.push('review') };
  readonly remove: RowAction = { id: 'delete', label: 'Eliminar', icon: 'pi pi-trash', testId: 'row-delete', destructive: true, run: () => this.calls.push('delete') };
}

describe('RowActionsComponent', () => {
  async function render(pick: (host: HostComponent) => RowAction[]): Promise<{ host: HostComponent; root: HTMLElement }> {
    stubMatchMedia();
    configureFeatureTest();

    const fixture = TestBed.createComponent(HostComponent);

    fixture.componentInstance.actions = pick(fixture.componentInstance);

    await settle(fixture);

    return { host: fixture.componentInstance, root: fixture.nativeElement as HTMLElement };
  }

  async function openMenu(root: HTMLElement): Promise<void> {
    const trigger = root.querySelector<HTMLButtonElement>('[data-testid="row-actions-trigger"]');

    trigger?.click();

    // The popup is appended to the body and rendered on its own schedule: wait for it, do not guess a delay.
    await vi.waitFor(() => expect(document.body.querySelector('[role="menu"]')).not.toBeNull());
  }

  function item(testId: string): HTMLElement | null {
    return document.body.querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  }

  function inline(root: HTMLElement, testId: string): HTMLElement | null {
    return root.querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  }

  afterEach(() => {
    // A popup appended to the body outlives its fixture: remove it by role, whatever the library calls its wrapper.
    document.body.querySelectorAll('[role="menu"]').forEach((menu) => {
      let root: Element = menu;

      while (root.parentElement !== null && root.parentElement !== document.body) {
        root = root.parentElement;
      }

      root.remove();
    });
  });

  describe('with fewer than three actions', () => {
    it('shows one icon-only button with its label as accessible name and no menu trigger', async () => {
      const { root } = await render((host) => [host.edit]);

      const button = inline(root, 'row-edit');

      expect(button?.getAttribute('aria-label')).toBe('Editar');
      expect(button?.querySelector('.pi-pencil')).not.toBeNull();
      expect(button?.textContent?.trim()).toBe('');
      expect(inline(root, 'row-actions-trigger')).toBeNull();
    });

    it('shows two icon-only buttons and runs the one that is clicked', async () => {
      const { host, root } = await render((current) => [current.edit, current.remove]);

      expect(inline(root, 'row-edit')?.textContent?.trim()).toBe('');
      expect(inline(root, 'row-delete')?.textContent?.trim()).toBe('');
      expect(inline(root, 'row-actions-trigger')).toBeNull();

      inline(root, 'row-delete')?.click();

      expect(host.calls).toEqual(['delete']);
    });

    it('does not run a disabled action', async () => {
      const { host, root } = await render((current) => [current.review]);

      const button = inline(root, 'row-review') as HTMLButtonElement | null;

      expect(button?.disabled).toBe(true);

      button?.click();

      expect(host.calls).toEqual([]);
    });

    it('renders a navigation action as a link', async () => {
      const { root } = await render(() => [{ id: 'open', label: 'Abrir', icon: 'pi pi-eye', testId: 'row-open', link: { commands: ['/runs', 'r1'], queryParams: { tab: 'x' } } }]);

      const link = inline(root, 'row-open');

      expect(link?.tagName).toBe('A');
      expect(link?.getAttribute('href')).toBe('/runs/r1?tab=x');
      expect(link?.getAttribute('aria-label')).toBe('Abrir');
    });

    it('translates a label key', async () => {
      const { root } = await render(() => [{ id: 'close', labelKey: 'common.back', icon: 'pi pi-times', testId: 'row-key', run: () => undefined }]);

      expect(inline(root, 'row-key')?.getAttribute('aria-label')).toBe('Atrás');
    });

    it('renders nothing when every action is hidden', async () => {
      const { root } = await render((host) => [{ ...host.edit, hidden: true }]);

      expect(root.querySelector('button, a')).toBeNull();
    });
  });

  describe('with three or more actions', () => {
    it('shows an icon-only trigger with a translated accessible name', async () => {
      const { root } = await render((host) => [host.edit, host.review, host.remove]);

      const trigger = root.querySelector('[data-testid="row-actions-trigger"]');

      expect(trigger?.getAttribute('aria-label')).toBe('Acciones de la fila');
      expect(trigger?.querySelector('.pi-ellipsis-v')).not.toBeNull();
      expect(trigger?.textContent?.trim()).toBe('');
      expect(inline(root, 'row-edit')).toBeNull();
    });

    it('lists every action with its label and runs the one that is clicked', async () => {
      const { host, root } = await render((current) => [current.edit, current.review, current.remove]);

      await openMenu(root);

      expect(item('row-edit')?.textContent).toContain('Editar');
      expect(item('row-delete')?.textContent).toContain('Eliminar');

      item('row-delete')?.click();

      expect(host.calls).toEqual(['delete']);
    });

    it('keeps a disabled action listed but does not run it', async () => {
      const { host, root } = await render((current) => [current.edit, current.review, current.remove]);

      await openMenu(root);

      expect(item('row-review')?.closest('[role="menuitem"]')?.getAttribute('aria-disabled')).toBe('true');

      item('row-review')?.click();

      expect(host.calls).toEqual([]);
    });

    it('does not count hidden actions toward the menu threshold', async () => {
      const { root } = await render((host) => [host.edit, { ...host.review, hidden: true }, host.remove]);

      expect(inline(root, 'row-actions-trigger')).toBeNull();
      expect(inline(root, 'row-edit')).not.toBeNull();
      expect(inline(root, 'row-review')).toBeNull();
      expect(inline(root, 'row-delete')).not.toBeNull();
    });
  });
});
