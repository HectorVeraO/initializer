import { definePreset } from '@openng/optimus-ui-themes';
import Aura from '@openng/optimus-ui-themes/aura';

export const AppPreset = definePreset(Aura, {
  primitive: {
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
        formField: {
          focusBorderColor: '{primary.400}'
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
        formField: {
          focusBorderColor: '{primary.400}'
        }
      }
    }
  },
  components: {
    datatable: {
      headerCell: {
        background: '{surface.50}',
        color: '{surface.700}'
      }
    },
    select: {
      option: {
        selectedBackground: '{primary.50}',
        selectedColor: '{primary.500}',
        selectedFocusBackground: '{primary.50}',
        selectedFocusColor: '{primary.500}'
      }
    },
    tag: {
      root: {
        fontSize: '12px',
        fontWeight: '500',
        padding: '8px',
        gap: '4px',
        borderRadius: '8px'
      },
      colorScheme: {
        light: {
          success: {
            background: '#F0FDF4',
            color: '#16A34A'
          },
          danger: {
            background: '#FEF2F2',
            color: '#DC2626'
          },
          secondary: {
            background: '#F5F5F5',
            color: '#737373'
          },
          info: {
            background: '#DBEAFE',
            color: '#2563EB'
          },
          warn: {
            background: '#FFF7ED',
            color: '#CA3500'
          }
        }
      }
    }
  }
});
