import { definePreset } from '@openng/optimus-ui-themes';
import Aura from '@openng/optimus-ui-themes/aura';

interface DesignTokenHelpers {
  dt: (token: string) => string | number | undefined;
}

const SHADOW_LG = '0 4px 6px -2px rgba(0, 0, 0, 0.03), 0 12px 16px -4px rgba(0, 0, 0, 0.08)';

const SHADOW_XL = '0 8px 8px -4px rgba(0, 0, 0, 0.03), 0 20px 24px -4px rgba(0, 0, 0, 0.08)';

/**
 * initializer design system on top of Aura: the interface green primary scale,
 * neutral grayscale surfaces, Rubik, 8 px radius and the design token shadow
 * scale. Component tokens cover what the screens use; the `css` blocks add only
 * what the token set does not expose (borders on tags, cards and the table
 * container, the dialog footer divider, the gray disabled button, the paginator
 * layout, the segmented track and raised pill of the tabs).
 */
export const AppPreset = definePreset(Aura, {
  primitive: {
    borderRadius: {
      none: '0',
      xs: '4px',
      sm: '6px',
      md: '8px',
      lg: '10px',
      xl: '12px'
    },
    brand: {
      50: '#effbf7',
      100: '#d7f3e6',
      200: '#b0e7cd',
      300: '#8adbbe',
      400: '#3eba8e',
      500: '#32a179',
      600: '#257d5d',
      700: '#1c5f47',
      800: '#164a37',
      900: '#0c2b20',
      950: '#071a13'
    },
    success: {
      50: '#f0fdf4',
      100: '#dcfce7',
      200: '#bbf7d0',
      300: '#86efac',
      400: '#4ade80',
      500: '#22c55e',
      600: '#16a34a',
      700: '#15803d',
      800: '#166534',
      900: '#14532d',
      950: '#052e16'
    },
    danger: {
      50: '#fef2f2',
      100: '#fee2e2',
      200: '#fecaca',
      300: '#fca5a5',
      400: '#f87171',
      500: '#ef4444',
      600: '#dc2626',
      700: '#b91c1c',
      800: '#991b1b',
      900: '#7f1d1d',
      950: '#450a0a'
    }
  },
  semantic: {
    primary: {
      50: '{brand.50}',
      100: '{brand.100}',
      200: '{brand.200}',
      300: '{brand.300}',
      400: '{brand.400}',
      500: '{brand.500}',
      600: '{brand.600}',
      700: '{brand.700}',
      800: '{brand.800}',
      900: '{brand.900}',
      950: '{brand.950}'
    },
    focusRing: {
      width: '2px',
      style: 'solid',
      color: '{primary.300}',
      offset: '2px',
      shadow: 'none'
    },
    formField: {
      paddingX: '12px',
      paddingY: '7px',
      sm: {
        fontSize: '12px',
        paddingX: '10px',
        paddingY: '6px'
      },
      lg: {
        fontSize: '16px',
        paddingX: '14px',
        paddingY: '8px'
      },
      borderRadius: '{border.radius.md}',
      focusRing: {
        width: '0',
        style: 'none',
        color: 'transparent',
        offset: '0',
        shadow: '0 0 0 3px color-mix(in srgb, {primary.300}, transparent 70%)'
      }
    },
    content: {
      borderRadius: '{border.radius.md}'
    },
    list: {
      option: {
        borderRadius: '{border.radius.sm}'
      }
    },
    navigation: {
      item: {
        borderRadius: '{border.radius.md}'
      }
    },
    overlay: {
      select: {
        borderRadius: '{border.radius.md}',
        shadow: SHADOW_LG
      },
      popover: {
        borderRadius: '{border.radius.md}',
        shadow: SHADOW_LG
      },
      modal: {
        borderRadius: '{border.radius.xl}',
        padding: '24px',
        shadow: SHADOW_XL
      },
      navigation: {
        shadow: SHADOW_LG
      }
    },
    colorScheme: {
      light: {
        surface: {
          0: '#ffffff',
          50: '{neutral.50}',
          100: '{neutral.100}',
          200: '{neutral.200}',
          300: '{neutral.300}',
          400: '{neutral.400}',
          500: '{neutral.500}',
          600: '{neutral.600}',
          700: '{neutral.700}',
          800: '{neutral.800}',
          900: '{neutral.900}',
          950: '{neutral.950}'
        },
        primary: {
          color: '{primary.600}',
          contrastColor: '#ffffff',
          hoverColor: '{primary.700}',
          activeColor: '{primary.800}'
        },
        highlight: {
          background: '{primary.50}',
          focusBackground: '{primary.100}',
          color: '{primary.700}',
          focusColor: '{primary.800}'
        },
        formField: {
          background: '{surface.0}',
          disabledBackground: '{surface.100}',
          borderColor: '{surface.300}',
          hoverBorderColor: '{surface.400}',
          focusBorderColor: '{primary.400}',
          color: '{surface.900}',
          disabledColor: '{surface.400}',
          placeholderColor: '{surface.500}',
          iconColor: '{surface.500}'
        },
        text: {
          color: '{surface.900}',
          hoverColor: '{surface.950}',
          mutedColor: '{surface.500}',
          hoverMutedColor: '{surface.600}'
        },
        content: {
          background: '{surface.0}',
          hoverBackground: '{surface.50}',
          borderColor: '{surface.200}'
        },
        navigation: {
          item: {
            focusBackground: '{surface.100}',
            activeBackground: '{surface.100}'
          }
        }
      },
      dark: {
        surface: {
          0: '#ffffff',
          50: '{neutral.50}',
          100: '{neutral.100}',
          200: '{neutral.200}',
          300: '{neutral.300}',
          400: '{neutral.400}',
          500: '{neutral.500}',
          600: '{neutral.600}',
          700: '{neutral.700}',
          800: '{neutral.800}',
          900: '{neutral.900}',
          950: '{neutral.950}'
        },
        primary: {
          color: '{primary.500}',
          contrastColor: '#ffffff',
          hoverColor: '{primary.400}',
          activeColor: '{primary.300}'
        },
        highlight: {
          background: 'color-mix(in srgb, {primary.500}, transparent 84%)',
          focusBackground: 'color-mix(in srgb, {primary.500}, transparent 76%)',
          color: '{primary.400}',
          focusColor: '{primary.300}'
        },
        formField: {
          background: '{surface.900}',
          disabledBackground: '{surface.800}',
          borderColor: '{surface.700}',
          hoverBorderColor: '{surface.600}',
          focusBorderColor: '{primary.500}',
          color: '{surface.0}',
          disabledColor: '{surface.500}',
          placeholderColor: '{surface.500}',
          iconColor: '{surface.400}'
        },
        text: {
          color: '{surface.0}',
          hoverColor: '{surface.0}',
          mutedColor: '{surface.400}',
          hoverMutedColor: '{surface.300}'
        },
        content: {
          background: '{surface.900}',
          hoverBackground: '{surface.800}',
          borderColor: '{surface.800}'
        },
        overlay: {
          select: {
            background: '{surface.900}',
            borderColor: '{surface.700}'
          },
          popover: {
            background: '{surface.900}',
            borderColor: '{surface.700}'
          },
          modal: {
            background: '{surface.900}',
            borderColor: '{surface.800}'
          }
        }
      }
    }
  },
  components: {
    button: {
      root: {
        iconOnlyWidth: '36px',
        sm: {
          iconOnlyWidth: '32px'
        },
        lg: {
          iconOnlyWidth: '40px'
        },
        label: {
          fontWeight: '500'
        }
      },
      colorScheme: {
        light: {
          root: {
            primary: {
              focusRing: {
                color: '{primary.300}',
                shadow: 'none'
              }
            },
            secondary: {
              background: '{surface.0}',
              hoverBackground: '{surface.50}',
              activeBackground: '{surface.100}',
              borderColor: '{surface.300}',
              hoverBorderColor: '{surface.300}',
              activeBorderColor: '{surface.400}',
              color: '{surface.800}',
              hoverColor: '{surface.900}',
              activeColor: '{surface.950}',
              focusRing: {
                color: '{surface.300}',
                shadow: 'none'
              }
            },
            danger: {
              background: '{red.600}',
              hoverBackground: '{red.700}',
              activeBackground: '{red.800}',
              borderColor: '{red.600}',
              hoverBorderColor: '{red.700}',
              activeBorderColor: '{red.800}',
              focusRing: {
                color: '{red.300}',
                shadow: 'none'
              }
            }
          },
          outlined: {
            primary: {
              hoverBackground: '{primary.50}',
              activeBackground: '{primary.100}',
              borderColor: '{primary.300}',
              color: '{primary.700}'
            },
            secondary: {
              hoverBackground: '{surface.50}',
              activeBackground: '{surface.100}',
              borderColor: '{surface.300}',
              color: '{surface.800}'
            },
            danger: {
              hoverBackground: '{red.50}',
              activeBackground: '{red.100}',
              borderColor: '{red.300}',
              color: '{red.600}'
            }
          },
          text: {
            primary: {
              hoverBackground: 'transparent',
              activeBackground: 'transparent',
              color: '{primary.600}'
            },
            secondary: {
              hoverBackground: '{surface.100}',
              activeBackground: '{surface.200}',
              color: '{surface.600}'
            },
            danger: {
              hoverBackground: 'transparent',
              activeBackground: 'transparent',
              color: '{red.600}'
            }
          }
        },
        dark: {
          root: {
            secondary: {
              background: 'transparent',
              hoverBackground: '{surface.800}',
              activeBackground: '{surface.700}',
              borderColor: '{surface.600}',
              hoverBorderColor: '{surface.500}',
              activeBorderColor: '{surface.500}',
              color: '{surface.0}',
              hoverColor: '{surface.0}',
              activeColor: '{surface.0}',
              focusRing: {
                color: '{surface.500}',
                shadow: 'none'
              }
            },
            danger: {
              background: '{red.600}',
              hoverBackground: '{red.500}',
              activeBackground: '{red.400}',
              borderColor: '{red.600}',
              hoverBorderColor: '{red.500}',
              activeBorderColor: '{red.400}',
              color: '#ffffff',
              hoverColor: '#ffffff',
              activeColor: '#ffffff'
            }
          },
          outlined: {
            secondary: {
              hoverBackground: '{surface.800}',
              activeBackground: '{surface.700}',
              borderColor: '{surface.600}',
              color: '{surface.0}'
            }
          },
          text: {
            primary: {
              hoverBackground: 'transparent',
              activeBackground: 'transparent'
            },
            danger: {
              hoverBackground: 'transparent',
              activeBackground: 'transparent'
            }
          }
        }
      },
      css: ({ dt }: DesignTokenHelpers) => `
.p-button:disabled:not(.p-button-outlined):not(.p-button-text):not(.p-button-link) {
    opacity: 1;
    background: ${dt('surface.300')};
    border-color: ${dt('surface.300')};
    color: ${dt('surface.0')};
}
.app-dark .p-button:disabled:not(.p-button-outlined):not(.p-button-text):not(.p-button-link) {
    background: ${dt('surface.700')};
    border-color: ${dt('surface.700')};
    color: ${dt('surface.400')};
}
.p-button-text:not(.p-button-secondary):not(:disabled):hover .p-button-label {
    text-decoration: underline;
}
`
    },
    datatable: {
      headerCell: {
        padding: '12px 16px',
        gap: '6px',
        sm: {
          padding: '8px 12px'
        }
      },
      columnTitle: {
        fontWeight: '500'
      },
      bodyCell: {
        padding: '12px 16px',
        sm: {
          padding: '8px 12px'
        }
      },
      colorScheme: {
        light: {
          headerCell: {
            background: '{surface.50}',
            color: '{surface.700}'
          },
          row: {
            hoverBackground: '{surface.50}'
          }
        },
        dark: {
          headerCell: {
            background: '{surface.800}',
            color: '{surface.300}'
          },
          row: {
            hoverBackground: '{surface.800}'
          }
        }
      },
      css: ({ dt }: DesignTokenHelpers) => `
.p-datatable-table-container {
    border: 1px solid ${dt('datatable.border.color')};
    border-radius: ${dt('content.border.radius')};
}
.p-datatable-tbody > tr:last-child > td {
    border-bottom-width: 0;
}
.p-datatable-filter-add-rule-button {
    color: ${dt('primary.color')};
}
.p-datatable.nested-tests .p-datatable-table-container {
    border: 0;
    border-radius: 0;
}
.p-datatable.nested-tests .p-datatable-thead > tr > th {
    background: transparent;
    color: var(--p-text-muted-color);
    font-size: 12px;
}
.p-datatable.nested-tests .p-datatable-tbody > tr > td {
    background: transparent;
}
.p-datatable.nested-tests .p-paginator {
    padding: 8px 16px;
    font-size: 12px;
}
`
    },
    paginator: {
      root: {
        padding: '12px 0 0 0',
        gap: '4px',
        background: 'transparent'
      },
      navButton: {
        width: '32px',
        height: '32px',
        borderRadius: '{border.radius.sm}'
      },
      colorScheme: {
        light: {
          navButton: {
            hoverBackground: '{surface.100}',
            color: '{surface.700}',
            hoverColor: '{surface.900}'
          }
        },
        dark: {
          navButton: {
            hoverBackground: '{surface.800}',
            color: '{surface.300}',
            hoverColor: '{surface.0}'
          }
        }
      },
      css: ({ dt }: DesignTokenHelpers) => `
.p-paginator .p-paginator-current {
    order: 1;
    color: ${dt('text.muted.color')};
}
.p-paginator .p-paginator-rpp-dropdown {
    order: 2;
    margin-inline-end: auto;
}
.p-paginator .p-paginator-prev {
    order: 3;
}
.p-paginator .p-paginator-pages {
    order: 4;
}
.p-paginator .p-paginator-next {
    order: 5;
}
.p-paginator .p-paginator-prev,
.p-paginator .p-paginator-next {
    width: auto;
    padding: 0 12px;
    border: 1px solid ${dt('form.field.border.color')};
    color: ${dt('text.color')};
    font-weight: 500;
}
`
    },
    tag: {
      root: {
        fontSize: '12px',
        fontWeight: '500',
        padding: '2px 8px',
        gap: '4px',
        borderRadius: '{border.radius.sm}'
      },
      colorScheme: {
        light: {
          primary: {
            background: '{primary.50}',
            color: '{primary.700}'
          },
          secondary: {
            background: '{surface.100}',
            color: '{surface.600}'
          },
          success: {
            background: '{success.50}',
            color: '{success.700}'
          },
          info: {
            background: '{blue.50}',
            color: '{blue.700}'
          },
          warn: {
            background: '{orange.50}',
            color: '{orange.700}'
          },
          danger: {
            background: '{danger.50}',
            color: '{danger.700}'
          }
        },
        dark: {
          info: {
            background: 'color-mix(in srgb, {blue.500}, transparent 84%)',
            color: '{blue.300}'
          }
        }
      },
      css: ({ dt }: DesignTokenHelpers) => `
.p-tag {
    border: 1px solid transparent;
    line-height: 18px;
}
.p-tag-primary {
    border-color: ${dt('primary.200')};
}
.p-tag-secondary {
    border-color: ${dt('surface.200')};
}
.p-tag-success {
    border-color: ${dt('success.200')};
}
.p-tag-info {
    border-color: ${dt('blue.200')};
}
.p-tag-warn {
    border-color: ${dt('orange.200')};
}
.p-tag-danger {
    border-color: ${dt('danger.200')};
}
.app-dark .p-tag-primary,
.app-dark .p-tag-secondary,
.app-dark .p-tag-success,
.app-dark .p-tag-info,
.app-dark .p-tag-warn,
.app-dark .p-tag-danger {
    border-color: color-mix(in srgb, currentColor, transparent 60%);
}
`
    },
    avatar: {
      root: {
        width: '36px',
        height: '36px',
        fontSize: '12px',
        background: '{highlight.background}',
        color: '{highlight.color}'
      },
      lg: {
        width: '40px',
        height: '40px',
        fontSize: '14px'
      }
    },
    badge: {
      root: {
        borderRadius: '9999px',
        fontWeight: '500'
      }
    },
    dialog: {
      header: {
        padding: '20px 24px'
      },
      title: {
        fontSize: '16px',
        fontWeight: '500'
      },
      content: {
        padding: '0 24px 20px 24px'
      },
      footer: {
        padding: '16px 24px',
        gap: '8px'
      },
      css: ({ dt }: DesignTokenHelpers) => `
.p-dialog-footer {
    border-top: 1px solid ${dt('content.border.color')};
}
`
    },
    card: {
      root: {
        borderRadius: '{border.radius.lg}',
        shadow: 'none'
      },
      body: {
        padding: '20px 24px',
        gap: '12px'
      },
      title: {
        fontSize: '18px',
        fontWeight: '500'
      },
      css: ({ dt }: DesignTokenHelpers) => `
.p-card {
    border: 1px solid ${dt('content.border.color')};
}
`
    },
    panel: {
      root: {
        borderRadius: '{border.radius.lg}'
      },
      colorScheme: {
        light: {
          root: {
            borderColor: '{surface.300}'
          }
        },
        dark: {
          root: {
            borderColor: '{surface.700}'
          }
        }
      },
      header: {
        padding: '16px 20px',
        borderWidth: '0 0 1px 0'
      },
      title: {
        fontWeight: '500'
      },
      content: {
        padding: '20px'
      },
      footer: {
        padding: '0 20px 20px 20px'
      }
    },
    toggleswitch: {
      root: {
        width: '36px',
        height: '20px'
      },
      handle: {
        size: '14px'
      }
    },
    tabs: {
      tablist: {
        background: 'transparent',
        borderWidth: '1px',
        borderColor: '{content.border.color}'
      },
      tab: {
        background: 'transparent',
        hoverBackground: '{surface.200}',
        activeBackground: '{surface.0}',
        borderWidth: '0',
        margin: '0',
        padding: '6px 12px',
        gap: '6px',
        fontWeight: '500',
        color: '{text.muted.color}',
        hoverColor: '{text.color}',
        activeColor: '{surface.900}'
      },
      tabpanel: {
        background: 'transparent',
        padding: '24px 0 0 0'
      },
      activeBar: {
        height: '0',
        bottom: '0',
        background: 'transparent'
      },
      navButton: {
        width: '2rem',
        background: '{surface.100}'
      },
      colorScheme: {
        light: {
          navButton: {
            shadow: 'none'
          }
        },
        dark: {
          navButton: {
            background: '{surface.800}',
            shadow: 'none'
          },
          tablist: {
            borderColor: '{surface.700}'
          },
          tab: {
            hoverBackground: 'color-mix(in srgb, {surface.700}, transparent 50%)',
            activeBackground: '{surface.700}',
            activeColor: '{surface.0}'
          }
        }
      },
      css: ({ dt }: DesignTokenHelpers) => `
.p-tablist-tab-list {
    width: max-content;
    gap: 2px;
    padding: 3px;
    border-radius: ${dt('border.radius.md')};
    background: ${dt('surface.100')};
}
.app-dark .p-tablist-tab-list {
    background: ${dt('surface.800')};
}
.p-tab {
    border-radius: ${dt('border.radius.sm')};
    font-size: 13px;
    line-height: 20px;
}
.p-tab.p-tab-active {
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08), 0 1px 3px rgba(0, 0, 0, 0.06);
}
.app-dark .p-tab.p-tab-active {
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.4);
}
`
    },
    message: {
      content: {
        padding: '10px 14px',
        gap: '8px'
      },
      text: {
        fontSize: '14px',
        fontWeight: '400'
      }
    },
    tooltip: {
      root: {
        padding: '8px 12px',
        borderRadius: '{border.radius.md}',
        shadow: SHADOW_LG
      },
      colorScheme: {
        light: {
          root: {
            background: '{surface.900}',
            color: '{surface.0}'
          }
        },
        dark: {
          root: {
            background: '{surface.700}',
            color: '{surface.0}'
          }
        }
      }
    }
  }
});
