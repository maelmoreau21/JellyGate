import os
import re

def fix_css():
    f = 'web/static/css/custom.css'
    if not os.path.exists(f): return
    c = open(f, 'r', encoding='utf-8').read()
    
    # Fix section anchor
    c = re.sub(r'\.jg-section-anchor\s*{\s*display: inline-flex;[^}]+}', 
               '.jg-section-anchor { display: inline-flex; align-items: center; gap: 0.45rem; padding: 0.6rem 0.95rem; border-radius: 999px; border: 1px solid var(--jg-border); background: var(--jg-bg-secondary); color: var(--jg-text); font-size: 0.84rem; font-weight: 600; text-decoration: none; transition: var(--jg-transition); }', c)
    
    # Fix hover
    c = re.sub(r'\.jg-section-anchor:hover\s*{\s*border-color: rgba\(45, 212, 191, 0\.35\);[^}]+}', 
               '.jg-section-anchor:hover { border-color: var(--jg-border-strong); background: var(--jg-bg-card); }', c)

    # Remove radial gradients that look "AI-startup"
    c = re.sub(r'radial-gradient\(circle at top, rgba\(20, 184, 166, 0\.06\), transparent 45%\)', 'none', c)
    c = re.sub(r'linear-gradient\(135deg, #dff7ff, #7dd3fc 45%, #5eead4\)', 'var(--jg-text)', c)
    
    with open(f, 'w', encoding='utf-8') as fh:
        fh.write(c)
    print("Fixed custom.css")

fix_css()
