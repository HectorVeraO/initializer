import { MenuItem } from '@openng/optimus-ui/api';

/**
 * Developer guide menu section (Sakai dashboard and UI kit). The production
 * build replaces this file with `dev.menu.prod.ts`.
 */
export function createDeveloperSection(label: string): MenuItem | null {
  return {
    id: 'developer',
    label,
    items: [
      { label: 'Sakai Dashboard', icon: 'pi pi-fw pi-home', routerLink: ['/dev/dashboard'] },
      {
        label: 'Sakai UI Kit',
        items: [
          { label: 'Form Layout', icon: 'pi pi-fw pi-id-card', routerLink: ['/dev/uikit/formlayout'] },
          { label: 'Input', icon: 'pi pi-fw pi-check-square', routerLink: ['/dev/uikit/input'] },
          { label: 'Button', icon: 'pi pi-fw pi-mobile', class: 'rotated-icon', routerLink: ['/dev/uikit/button'] },
          { label: 'Table', icon: 'pi pi-fw pi-table', routerLink: ['/dev/uikit/table'] },
          { label: 'List', icon: 'pi pi-fw pi-list', routerLink: ['/dev/uikit/list'] },
          { label: 'Tree', icon: 'pi pi-fw pi-share-alt', routerLink: ['/dev/uikit/tree'] },
          { label: 'Panel', icon: 'pi pi-fw pi-tablet', routerLink: ['/dev/uikit/panel'] },
          { label: 'Overlay', icon: 'pi pi-fw pi-clone', routerLink: ['/dev/uikit/overlay'] },
          { label: 'Media', icon: 'pi pi-fw pi-image', routerLink: ['/dev/uikit/media'] },
          { label: 'Menu', icon: 'pi pi-fw pi-bars', routerLink: ['/dev/uikit/menu'] },
          { label: 'Message', icon: 'pi pi-fw pi-comment', routerLink: ['/dev/uikit/message'] },
          { label: 'File', icon: 'pi pi-fw pi-file', routerLink: ['/dev/uikit/file'] },
          { label: 'Chart', icon: 'pi pi-fw pi-chart-bar', routerLink: ['/dev/uikit/charts'] },
          { label: 'Timeline', icon: 'pi pi-fw pi-calendar', routerLink: ['/dev/uikit/timeline'] },
          { label: 'Misc', icon: 'pi pi-fw pi-circle', routerLink: ['/dev/uikit/misc'] }
        ]
      }
    ]
  };
}
