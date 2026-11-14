import type { Config } from "tailwindcss"

const config = {
  darkMode: ["class"],
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./lib/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        border: 'hsl(var(--border))',
        input: 'hsl(var(--input))',
        ring: 'hsl(var(--ring))',
        brand: 'hsl(var(--brand) / <alpha-value>)',
        background: 'hsl(var(--background))',
        'workspace-surface': 'hsl(var(--workspace-surface))',
        field: 'hsl(var(--field))',
        material: {
          chrome: 'hsl(var(--material-chrome))',
          workspace: 'hsl(var(--material-workspace))',
          floating: 'hsl(var(--material-floating))'
        },
        scrim: 'hsl(var(--scrim))',
        foreground: 'hsl(var(--foreground))',
        primary: {
          DEFAULT: 'hsl(var(--primary))',
          foreground: 'hsl(var(--primary-foreground))'
        },
        secondary: {
          DEFAULT: 'hsl(var(--secondary))',
          foreground: 'hsl(var(--secondary-foreground))'
        },
        destructive: {
          DEFAULT: 'hsl(var(--destructive))',
          foreground: 'hsl(var(--destructive-foreground))'
        },
        muted: {
          DEFAULT: 'hsl(var(--muted))',
          foreground: 'hsl(var(--muted-foreground))'
        },
        accent: {
          DEFAULT: 'hsl(var(--accent))',
          foreground: 'hsl(var(--accent-foreground))'
        },
        popover: {
          DEFAULT: 'hsl(var(--popover))',
          foreground: 'hsl(var(--popover-foreground))'
        },
        card: {
          DEFAULT: 'hsl(var(--card))',
          foreground: 'hsl(var(--card-foreground))'
        },
        sidebar: {
          DEFAULT: 'hsl(var(--sidebar-background))',
          foreground: 'hsl(var(--sidebar-foreground))',
          primary: 'hsl(var(--sidebar-primary))',
          'primary-foreground': 'hsl(var(--sidebar-primary-foreground))',
          accent: 'hsl(var(--sidebar-accent))',
          'accent-foreground': 'hsl(var(--sidebar-accent-foreground))',
          border: 'hsl(var(--sidebar-border))',
          ring: 'hsl(var(--sidebar-ring))'
        },
        success: {
          DEFAULT: 'hsl(var(--success))',
          foreground: 'hsl(var(--success-foreground))'
        },
        warning: {
          DEFAULT: 'hsl(var(--warning))',
          foreground: 'hsl(var(--warning-foreground))'
        },
        info: {
          DEFAULT: 'hsl(var(--info))',
          foreground: 'hsl(var(--info-foreground))'
        },
        chart: {
          '1': 'hsl(var(--chart-1))',
          '2': 'hsl(var(--chart-2))',
          '3': 'hsl(var(--chart-3))',
          '4': 'hsl(var(--chart-4))',
          '5': 'hsl(var(--chart-5))'
        }
      },
      borderRadius: {
        lg: 'var(--radius)',
        md: 'calc(var(--radius) - 2px)',
        sm: 'calc(var(--radius) - 4px)'
      },
      fontFamily: {
        sans: [
          'var(--font-inter)'
        ],
        mono: [
          'var(--font-geist-mono)'
        ]
      },
      boxShadow: {
        // Plain shadcn shadow-xs: no inset top highlight, which read as gloss on controls.
        control: '0 1px 2px 0 hsl(var(--shadow-color) / 0.05)',
        surface: 'inset 0 1px 0 hsl(var(--surface-highlight) / calc(var(--surface-highlight-opacity) * 0.72)), 0 1px 2px hsl(var(--shadow-color) / 0.06), 0 8px 24px hsl(var(--shadow-color) / 0.045)',
        floating: 'inset 0 1px 0 hsl(var(--surface-highlight) / calc(var(--surface-highlight-opacity) * 0.84)), 0 2px 8px hsl(var(--shadow-color) / 0.1), 0 18px 48px hsl(var(--shadow-color) / 0.14)',
        connected: 'inset 0 1px 0 hsl(var(--surface-highlight) / var(--surface-highlight-opacity)), 0 -1px 0 hsl(var(--workspace-surface)), 0 1px 3px hsl(var(--shadow-color) / 0.07)'
      },
      transitionDuration: {
        panel: '280ms'
      },
      transitionTimingFunction: {
        panel: 'cubic-bezier(0.32, 0.72, 0, 1)'
      },
      keyframes: {
        'accordion-down': {
          from: {
            height: '0'
          },
          to: {
            height: 'var(--radix-accordion-content-height)'
          }
        },
        'accordion-up': {
          from: {
            height: 'var(--radix-accordion-content-height)'
          },
          to: {
            height: '0'
          }
        },
        'skeleton-fade-in': {
          from: {
            opacity: '0.88'
          },
          to: {
            opacity: '1'
          }
        },
        'apple-fade-in': {
          from: {
            opacity: '0.88',
            transform: 'scale(0.997)'
          },
          to: {
            opacity: '1',
            transform: 'scale(1)'
          }
        }
      },
      animation: {
        'accordion-down': 'accordion-down 0.2s ease-out',
        'accordion-up': 'accordion-up 0.2s ease-out',
        'skeleton-fade-in': 'skeleton-fade-in 150ms ease-out',
        'apple-fade-in': 'apple-fade-in 160ms cubic-bezier(0.32, 0.72, 0, 1)'
      }
    }
  },
  plugins: [require("tailwindcss-animate")],
} satisfies Config

export default config
