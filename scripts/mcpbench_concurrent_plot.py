"""One figure for `scripts/mcpbench.py --concurrent` results, drawn in
blocks (a light pixel style): every block is a fixed amount, so the
difference is counted rather than read off an axis.

    python scripts/mcpbench_concurrent_plot.py <concurrent.json> <out dir>

Three panels under one title:
  memory    per number of conversations, a tower of 100 MB blocks for
            native and for MCPcell
  wait      per MCP, a row of 1 s blocks from opening a conversation to
            the first result (median)
  done      per number of conversations, a tower of 0.5 s blocks until
            every call is done

Writes summary.svg and a 2x summary.png. Memory and time to finish are means
of the repetitions. Native is orange, MCPcell blue (the first two slots of
the dataviz reference palette, validated for colour-vision deficiencies).
"""
import json, math, os, statistics, sys

import matplotlib

matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.patches import Rectangle

CELL, NATIVE = '#2a78d6', '#eb6834'
SURFACE, INK, INK2, BASE, GHOST = '#fcfcfb', '#0b0b0b', '#52514e', '#cfcec8', '#f0efea'
MODES = ('native', 'mcpcell')
COLORS = {'native': NATIVE, 'mcpcell': CELL}
NAMES = {'native': '原生', 'mcpcell': 'MCPcell'}
MONO = ['Cascadia Mono', 'Consolas', 'Microsoft YaHei']
GAP = 0.16  # surface between blocks, in blocks
plt.rcParams.update({
    'font.family': ['Microsoft YaHei', 'DejaVu Sans'],
    'axes.facecolor': SURFACE, 'figure.facecolor': SURFACE, 'savefig.facecolor': SURFACE,
    'svg.fonttype': 'none',
})


def block(ax, x, y, color):
    ax.add_patch(Rectangle((x + GAP / 2, y + GAP / 2), 1 - GAP, 1 - GAP, facecolor=color, edgecolor='none'))


def count(v, unit):
    """Blocks for v: rounded, at least one for anything measured."""
    return max(1, round(v / unit)) if v > 0 else 0


def tower(ax, x, n, color, wide=2):
    """n blocks stacked `wide` to a row from the baseline; returns its height."""
    for i in range(n):
        r, c = divmod(i, wide)
        block(ax, x + c, r, color)
    return math.ceil(n / wide)


def badge(ax, x, y, text, ha='center'):
    ax.text(x, y, text, ha=ha, va='center', fontsize=12.5, fontweight='bold', color='white', family=MONO,
            bbox=dict(boxstyle='square,pad=0.35', facecolor=CELL, edgecolor='none'))


def value(ax, x, y, text, **kw):
    ax.text(x, y, text, fontsize=12, fontweight='bold', color=INK, family=MONO, **kw)


def frame(ax, w, h, title, key):
    ax.set_xlim(-0.5, w + 0.5)
    ax.set_ylim(-3.2, h + 4.2)
    ax.set_aspect('equal')
    ax.set_anchor('S')
    ax.axis('off')
    ax.text(-0.3, h + 3.2, title, fontsize=15, fontweight='bold', color=INK, va='center')
    ax.text(-0.3, h + 1.7, key, fontsize=10.5, color=INK2, va='center')


GROUP, WIDE = 6.4, 2


def towers_width(ns):
    return GROUP * len(ns) - (GROUP - 2 * WIDE - 0.8)


def towers(ax, ns, v, unit, fmt, title, key, top, badge_text):
    """Per conversation count, a native and an MCPcell tower side by side."""
    group, wide = GROUP, WIDE
    w = towers_width(ns)
    frame(ax, w, top, title, key)
    ax.plot([-0.3, w + 0.3], [0, 0], color=BASE, linewidth=1.2, solid_capstyle='butt')
    for i, n in enumerate(ns):
        gx = i * group
        for j, m in enumerate(MODES):
            x = gx + j * (wide + 0.8)
            h = tower(ax, x, count(v[m][i], unit), COLORS[m], wide)
            value(ax, x + wide / 2, h + 0.7, fmt(v[m][i]), ha='center', va='bottom')
        ax.text(gx + wide + 0.4, -1.4, f'{n} 个对话', ha='center', va='center', fontsize=11.5, color=INK2)
    badge(ax, w + 0.3, top + 3.2, badge_text, ha='right')


def main():
    data = json.load(open(sys.argv[1], encoding='utf-8'))
    out = sys.argv[2]
    os.makedirs(out, exist_ok=True)
    rows = data['concurrent']
    ns = sorted({r['n'] for r in rows})
    servers = list(rows[0]['servers'])

    def mean(key, m):
        return [statistics.mean(r[key] for r in rows if r['n'] == n and r['mode'] == m) for n in ns]

    mem = {m: mean('mb', m) for m in MODES}
    done = {m: mean('makespan', m) for m in MODES}
    firsts = {m: {s: [] for s in servers} for m in MODES}
    for r in rows:
        for s, v in r['servers'].items():
            firsts[r['mode']][s] += [a + b for a, b in zip(v['ready'], v['first'])]
    wait = {m: {s: statistics.median(firsts[m][s]) for s in servers if firsts[m][s]} for m in MODES}
    order = sorted(servers, key=lambda s: -wait['native'].get(s, 0))

    # One block size and one height for all three panels, so titles and
    # baselines line up and a block is the same size everywhere.
    wait_w = max(count(v, 1) for v in wait['native'].values())
    wait_h = len(order) * 3 - 1
    top = max(math.ceil(count(max(mem['native']), 100) / 2), math.ceil(count(max(done['native']), 0.5) / 2), wait_h)
    label_w = 8.5
    widths = [towers_width(ns), label_w + wait_w + 4, towers_width(ns)]

    fig = plt.figure(figsize=(19, 8.2))
    gs = fig.add_gridspec(1, 3, width_ratios=[w + 1 for w in widths], wspace=0.08)
    a, b, c = (fig.add_subplot(gs[0, i]) for i in range(3))

    # Memory.
    towers(a, ns, mem, 100, lambda v: f'{v / 1024:.1f} GB', '内存', '1 格 = 100 MB', top,
           f'省 {1 - mem["mcpcell"][-1] / mem["native"][-1]:.0%}')

    # Wait until each MCP gives its first result: a row per MCP and mode.
    frame(b, label_w + wait_w + 4, top, '等 MCP 能用', '1 格 = 1 秒')
    b.plot([label_w - 0.2, label_w - 0.2], [0, top], color=BASE, linewidth=1.2, solid_capstyle='butt')
    pitch = (top - 1) / len(order)  # the MCPs spread over the panel's height
    for i, s in enumerate(order):
        y0 = top - pitch * (i + 0.5)  # native row on top, MCPcell under it
        b.text(label_w - 0.8, y0, s, ha='right', va='center', fontsize=11.5, color=INK)
        for j, m in enumerate(MODES):
            v = wait[m].get(s)
            if v is None:
                continue
            yrow = y0 - j * 1.0 - 0.5
            n = count(v, 1)
            for k in range(n):
                block(b, label_w + k, yrow, COLORS[m])
            value(b, label_w + n + 0.4, yrow + 0.5, f'{v:.1f}s' if v >= 1 else f'{v:.2f}s', ha='left', va='center')
    slow = [s for s in order if wait['native'].get(s, 0) >= 5]
    ratio = statistics.median(wait['native'][s] / wait['mcpcell'][s] for s in slow) if slow else 1
    badge(b, label_w + wait_w + 4.3, top + 3.2, f'快 {ratio:.0f} 倍', ha='right')

    # Time until every call is done.
    towers(c, ns, done, 0.5, lambda v: f'{v:.1f}s', '全部做完', '1 格 = 0.5 秒', top,
           f'快 {done["native"][-1] / done["mcpcell"][-1]:.1f} 倍')

    # One title and the two colours.
    fig.text(0.125, 0.97, f'{len(servers)} 个 MCP 同时被调用：原生 vs MCPcell', fontsize=22, fontweight='bold', color=INK, va='bottom')
    x = 0.735
    for m in MODES:
        fig.patches.append(Rectangle((x, 0.978), 0.011, 0.024, transform=fig.transFigure, facecolor=COLORS[m], edgecolor='none'))
        fig.text(x + 0.016, 0.99, NAMES[m], fontsize=14, color=INK, va='center')
        x += 0.08

    fig.savefig(os.path.join(out, 'summary.svg'), bbox_inches='tight')
    fig.savefig(os.path.join(out, 'summary.png'), dpi=200, bbox_inches='tight')
    plt.close(fig)
    print('wrote', os.path.join(out, 'summary.png'))


if __name__ == '__main__':
    main()
