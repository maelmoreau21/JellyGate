module.exports = {
    content: ["./data/html/*.html", "./build/data/html/*.html", "./ts/*.ts", "./ts/modules/*.ts", "./html/*.html", "./views.go"],
    darkMode: 'class',
    theme: {
        extend: {
            colors: {
                glass: {
                    100: 'rgba(255, 255, 255, 0.1)',
                    200: 'rgba(255, 255, 255, 0.05)',
                    border: 'rgba(255, 255, 255, 0.15)',
                    dark: 'rgba(20, 20, 20, 0.65)'
                },
                primary: {
                    400: '#a78bfa',
                    500: '#8b5cf6',
                    600: '#7c3aed',
                },
                background: '#0a0a0a',
                surface: '#171717',
            },
            animation: {
                'fade-in': 'fadeIn 0.4s ease-out forwards',
                'slide-up': 'slideUp 0.5s cubic-bezier(0.16, 1, 0.3, 1) forwards',
                'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
            },
            keyframes: {
                fadeIn: {
                    '0%': { opacity: '0' },
                    '100%': { opacity: '1' },
                },
                slideUp: {
                    '0%': { transform: 'translateY(10px)', opacity: '0' },
                    '100%': { transform: 'translateY(0)', opacity: '1' },
                }
            },
            backdropBlur: {
                xs: '2px',
                glass: '12px',
            }
        }
    },
    plugins: [],
}
