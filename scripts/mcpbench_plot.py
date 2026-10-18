"""Figures for scripts/mcpbench.py results (and Codex runs).

    python scripts/mcpbench_plot.py <bench.json> <out dir> [--readiness r.json] [--codex requested.json result.json ...]

requested.json says what each Codex session was asked to use: {"A": [server, ...]}.

Writes fig1_scaling, fig2_readiness, fig3_latency and (with Codex results)
fig4_codex as PDF, SVG and 300 dpi PNG, plus captions.md. Symmetric data are
mean ± SD; skewed timings on log axes are median with min–max. Every
repetition is drawn as a point.
"""
import json, os, sys
import numpy as np
import matplotlib

matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.ticker import FuncFormatter

# Okabe-Ito, distinguishable with colour-vision deficiencies.
NATIVE, CELL, WARM, GREY = '#D55E00', '#0072B2', '#56B4E9', '#7F7F7F'
plt.rcParams.update({
    'font.family': ['Microsoft YaHei', 'DejaVu Sans'],
    'font.size': 8.5, 'axes.titlesize': 9, 'axes.labelsize': 8.5, 'legend.fontsize': 7.5,
    'xtick.labelsize': 7.5, 'ytick.labelsize': 7.5,
    'axes.linewidth': 0.7, 'xtick.major.width': 0.7, 'ytick.major.width': 0.7,
    'axes.spines.top': False, 'axes.spines.right': False,
    'axes.grid': True, 'grid.color': '#E5E5E5', 'grid.linewidth': 0.5, 'axes.axisbelow': True,
    'legend.frameon': False, 'savefig.dpi': 300, 'savefig.bbox': 'tight', 'svg.fonttype': 'none', 'pdf.fonttype': 42,
})
SINGLE, DOUBLE = 3.5, 7.2  # inches
PLAIN = FuncFormatter(lambda v, _: f'{v:g}')


def save(fig, out, name):
    for ext in ('pdf', 'svg', 'png'):
        fig.savefig(os.path.join(out, f'{name}.{ext}'))
    plt.close(fig)


def panel(ax, letter):
    ax.text(-0.17, 1.03, letter, transform=ax.transAxes, fontsize=10, fontweight='bold', va='bottom')


def mean_sd(values):
    v = [x for x in values if x is not None]
    return (float(np.mean(v)), float(np.std(v, ddof=1)) if len(v) > 1 else 0.0) if v else (np.nan, np.nan)


def fig_scaling(bench, out):
    rows = bench['scaling']
    ns = sorted({r['n'] for r in rows})
    fig, axes = plt.subplots(1, 3, figsize=(DOUBLE, 2.4), constrained_layout=True)
    metrics = [('mb', '内存（私有提交，GB）', 1 / 1024, False), ('processes', '进程数', 1, False),
               ('allReady', '全部就绪耗时（s，对数轴）', 1, True)]
    stats = {}
    for ax, (key, ylabel, scale, log), letter in zip(axes, metrics, 'abc'):
        for mode, color, marker, name in (('native', NATIVE, 'o', '原生'), ('mcpcell', CELL, 's', 'MCPcell')):
            xs, ms, sds = [], [], []
            for n in ns:
                vals = [r[key] * scale for r in rows if r['n'] == n and r['mode'] == mode]
                if not vals:
                    continue
                m, sd = mean_sd(vals)
                stats[(key, mode, n)] = m
                xs.append(n), ms.append(m), sds.append(sd)
                ax.scatter([n + (-0.07 if mode == 'native' else 0.07)] * len(vals), vals, s=7, color=color, alpha=0.35,
                           linewidths=0, zorder=2)
            ax.errorbar(xs, ms, yerr=sds, color=color, marker=marker, ms=4, lw=1.2, capsize=2.5, label=name, zorder=3)
        ax.set_xticks(ns)
        ax.set_xlabel('并发会话数（每个会话 9 个 MCP）')
        ax.set_ylabel(ylabel)
        if log:
            ax.set_yscale('log')
            ax.yaxis.set_major_formatter(PLAIN)
        panel(ax, letter)
    lo, hi = min(ns), max(ns)
    a = axes[0]
    a.annotate(f'{stats[("mb", "native", hi)]:.1f} GB', (hi, stats[('mb', 'native', hi)]), textcoords='offset points', xytext=(-6, 6),
               ha='right', fontsize=7, color=NATIVE)
    for n in (lo, hi):
        a.annotate(f'{stats[("mb", "mcpcell", n)] * 1024:.0f} MB', (n, stats[('mb', 'mcpcell', n)]), textcoords='offset points',
                   xytext=(0, 7), ha='center', fontsize=7, color=CELL)
    ratio = stats[('mb', 'native', hi)] / stats[('mb', 'mcpcell', hi)]
    a.text(0.97, 0.3, f'{hi} 个会话时\n原生是 MCPcell 的 {ratio:.0f} 倍', transform=a.transAxes, fontsize=7, color=GREY, ha='right')
    a.legend(loc='upper left')
    save(fig, out, 'fig1_scaling')
    reps = max(r['rep'] for r in rows) + 1
    return (f'**图 1｜并发会话的开销。** 每个会话配 9 个 MCP，已连接并列出工具、未调用。(a) 所有相关进程的私有提交之和'
            f'（原生：各 MCP 的进程树，含 conhost；MCPcell：转发程序、守护进程及其实例）；(b) 进程数；(c) 从开始启动到所有会话的全部 MCP 可用。'
            f'均值 ± 标准差，n = {reps}，点为单次结果。')


def fig_readiness(rows, servers, out):
    fig, ax = plt.subplots(figsize=(SINGLE, 3.0), constrained_layout=True)
    y = np.arange(len(servers))
    for mode, color, marker, dy, name in (('native', NATIVE, 'o', 0.14, '原生'), ('mcpcell', CELL, 's', -0.14, 'MCPcell（能力目录已缓存）')):
        for i, s in enumerate(servers):
            vals = [t for r in rows if r['mode'] == mode for t in r['ready'].get(s, []) if t]
            if not vals:
                continue
            med = float(np.median(vals))
            ax.scatter(vals, [y[i] + dy] * len(vals), s=7, color=color, alpha=0.35, linewidths=0)
            ax.errorbar([med], [y[i] + dy], xerr=[[med - min(vals)], [max(vals) - med]], fmt=marker, color=color, ms=4, capsize=2,
                        lw=1, label=name if i == 0 else None)
    ax.set_yticks(y, servers)
    ax.invert_yaxis()
    ax.set_xscale('log')
    ax.xaxis.set_major_formatter(PLAIN)
    ax.set_xlabel('从启动到列出工具（s，对数轴）')
    ax.legend(loc='lower center', bbox_to_anchor=(0.45, 1.0), ncol=2, handletextpad=0.3, columnspacing=1.2)
    ax.grid(axis='y', visible=False)
    save(fig, out, 'fig2_readiness')
    n = len({r['rep'] for r in rows})
    return (f'**图 2｜单个会话里每个 MCP 的就绪时间**：从启动客户端进程到 tools/list 返回；一个会话的 9 个 MCP 同时启动，与 Agent 一致，'
            f'每个客户端单独计时。中位数，误差线为最小–最大值，n = {n}，点为单次结果。')


def fig_latency(bench, out):
    rows = bench['latency']
    servers = [s for s in bench['servers'] if any(r['server'] == s for r in rows)]
    variants = [('native', NATIVE, '原生'), ('mcpcell-cold', CELL, 'MCPcell，实例未运行'), ('mcpcell-warm', WARM, 'MCPcell，共享实例已在运行')]
    fig, (a, b) = plt.subplots(1, 2, figsize=(DOUBLE, 2.9), constrained_layout=True, gridspec_kw={'width_ratios': [1.15, 1]})
    x = np.arange(len(servers))
    w = 0.26
    seen = set()
    for k, (v, color, name) in enumerate(variants):
        for i, s in enumerate(servers):
            r = next((r for r in rows if r['server'] == s and r['variant'] == v), None)
            xpos = x[i] + (k - 1) * w
            if r and not r['first'] and r['error']:
                a.text(xpos, 0.03, '×', ha='center', va='bottom', color=color, fontsize=9, fontweight='bold')
            if not r or not r['first']:
                continue
            vals = r['first']
            med = float(np.median(vals))
            a.bar(xpos, med, w * 0.92, color=color, label=name if v not in seen else None)
            seen.add(v)
            a.errorbar([xpos], [med], yerr=[[med - min(vals)], [max(vals) - med]], fmt='none', ecolor='black', elinewidth=0.6, capsize=1.5)
            a.scatter([xpos] * len(vals), vals, s=5, color='black', alpha=0.45, linewidths=0, zorder=3)
    a.set_yscale('log')
    a.set_ylim(0.02, None)
    a.yaxis.set_major_formatter(PLAIN)
    a.set_xticks(x, servers, rotation=25, ha='right')
    a.set_ylabel('从启动到第一个结果（s，对数轴）')
    a.legend(loc='lower center', bbox_to_anchor=(0.5, 1.0), ncol=3, fontsize=6.8, handlelength=1.2, columnspacing=0.8)
    a.grid(axis='x', visible=False)
    panel(a, 'a')
    data, pos, colors, deltas = [], [], [], []
    for i, s in enumerate(servers):
        meds = {}
        for k, (v, color) in enumerate((('native', NATIVE), ('mcpcell-cold', CELL))):
            r = next((r for r in rows if r['server'] == s and r['variant'] == v), None)
            if r and r['rtt']:
                ms = [t * 1000 for t in r['rtt']]
                data.append(ms), pos.append(i + (k - 0.5) * 0.36), colors.append(color)
                meds[v] = float(np.median(ms))
        if len(meds) == 2:
            top = max(max(d) for d, p in zip(data, pos) if abs(p - i) < 0.5)
            deltas.append((i, meds['mcpcell-cold'] - meds['native'], top))
    bp = b.boxplot(data, positions=pos, widths=0.3, patch_artist=True, showfliers=False, medianprops={'color': 'black', 'lw': 0.8},
                   whiskerprops={'lw': 0.6}, capprops={'lw': 0.6}, boxprops={'lw': 0.6})
    for patch, c in zip(bp['boxes'], colors):
        patch.set_facecolor(c), patch.set_alpha(0.75)
    for i, d, top in deltas:
        b.text(i, top * 1.35, f'{d:+.1f} ms', ha='center', fontsize=6.5, color=GREY)
    b.set_yscale('log')
    b.yaxis.set_major_formatter(PLAIN)
    b.set_xticks(range(len(servers)), servers, rotation=25, ha='right')
    b.set_ylabel('单次调用往返（ms，对数轴）')
    b.set_ylim(None, max(max(d) for d in data) * 4)
    b.grid(axis='x', visible=False)
    b.legend(handles=[plt.Rectangle((0, 0), 1, 1, color=NATIVE, alpha=0.75), plt.Rectangle((0, 0), 1, 1, color=CELL, alpha=0.75)],
             labels=['原生', 'MCPcell'], loc='lower center', bbox_to_anchor=(0.5, 1.0), ncol=2)
    panel(b, 'b')
    save(fig, out, 'fig3_latency')
    reps = max((len(r['first']) for r in rows), default=0)
    own = sorted({r['server'] for r in rows} - {r['server'] for r in rows if r['variant'] == 'mcpcell-warm'})
    failed = sorted({r['server'] for r in rows if r['variant'] == 'native' and r['error']})
    note = (f'× 表示原生运行时调用失败：{"、".join(failed)} 读到环境里一个失效的 GitHub 令牌；MCPcell 的共享实例只拿白名单内的环境变量，'
            f'所以没受影响（这是环境差异，不是速度差异）。') if failed else ''
    return (f'**图 3｜调用速度。** (a) 从启动客户端到拿到第一个工具结果，含进程启动；“共享实例已在运行”指另一个会话已经启动了它，'
            f'只适用于共享服务（{"、".join(own)} 按会话运行，没有这一项）。中位数，误差线为最小–最大值，n = {reps}，点为单次结果。{note}'
            f'(b) 同一会话里连续调用的往返时间，箱为四分位距，须为 1.5 倍四分位距；本地工具 30 次，网络工具 10 次；标注为 MCPcell 与原生的中位数之差，网络工具（context7）的差值主要是网络波动。')


def fig_codex(results, requested, out):
    """requested: what each Codex session was asked to use, {"A": [server, ...]}."""
    by = {'native': [], 'wrap': []}
    for r in results:
        by[r['mode']].append(r)
    names = [('native', NATIVE, '原生'), ('wrap', CELL, 'MCPcell')]
    fig, axes = plt.subplots(1, 3, figsize=(DOUBLE, 2.4), constrained_layout=True)
    ax = axes[0]
    means = []
    for i, (m, c, _) in enumerate(names):
        vals = [r['peakSum'] / 1024 for r in by[m]]
        mu, sd = mean_sd(vals)
        means.append(mu)
        ax.bar(i, mu, 0.6, color=c, yerr=sd, capsize=3, error_kw={'lw': 0.7})
        ax.scatter([i] * len(vals), vals, s=9, color='black', alpha=0.5, linewidths=0, zorder=3)
        ax.text(i, mu + max(sd, 0.05) + 0.12, f'{mu:.2f} GB', ha='center', fontsize=7)
    ax.text(0.5, 0.55, f'−{(1 - means[1] / means[0]) * 100:.0f}%', transform=ax.transAxes, ha='center', fontsize=9, color=GREY)
    ax.set_xticks([0, 1], [n for _, _, n in names])
    ax.set_ylabel('MCP 内存峰值（GB）')
    ax.grid(axis='x', visible=False)
    panel(ax, 'a')
    ax = axes[1]
    palette = {'node.exe': '#009E73', 'cmd.exe': '#E69F00', 'conhost.exe': '#CC79A7', 'python.exe': '#F0E442',
               'mcpcell-run.exe': CELL, '其他': '#999999'}

    def count(r, k):
        if k != '其他':
            return r['by'].get(k, [0, 0])[0]
        return sum(c for name, (c, _mb) in r['by'].items() if name not in palette)

    for i, (m, _, _) in enumerate(names):
        bottom = 0
        for k in palette:
            v = float(np.mean([count(r, k) for r in by[m]])) if by[m] else 0
            if v:
                ax.bar(i, v, 0.6, bottom=bottom, color=palette[k], label=k.replace('.exe', ''))
                bottom += v
        ax.text(i, bottom + 1.5, f'{bottom:.0f}', ha='center', fontsize=7)
    handles, labels = ax.get_legend_handles_labels()
    uniq = dict(zip(labels, handles))
    ax.legend(uniq.values(), uniq.keys(), fontsize=6.5, loc='upper right', handlelength=1)
    ax.set_xticks([0, 1], [n for _, _, n in names])
    ax.set_ylabel('会话下的进程数')
    ax.grid(axis='x', visible=False)
    panel(ax, 'b')
    ax = axes[2]
    total = sum(len(v) for v in requested.values())
    for i, (m, c, _) in enumerate(names):
        vals = []
        for r in by[m]:
            used = 0
            for key, (_secs, _answer, calls) in r['results'].items():
                ok = {call.split('.')[0] for call in calls if call.endswith(':ok')}
                used += len(set(requested[key]) & ok)
            vals.append(used)
        mu, sd = mean_sd(vals)
        ax.bar(i, mu, 0.6, color=c, yerr=sd, capsize=3, error_kw={'lw': 0.7})
        ax.scatter([i] * len(vals), vals, s=9, color='black', alpha=0.5, linewidths=0, zorder=3)
        ax.text(i, mu + 0.2, f'{mu:.0f}/{total}', ha='center', fontsize=7)
    ax.set_ylim(0, total + 1)
    ax.set_xticks([0, 1], [n for _, _, n in names])
    ax.set_ylabel(f'成功用上的 MCP（共要求 {total} 个）')
    ax.grid(axis='x', visible=False)
    panel(ax, 'c')
    save(fig, out, 'fig4_codex')
    n = min(len(by['native']), len(by['wrap']))
    return (f'**图 4｜真实 Agent。** 3 个 Codex CLI 会话并发，每个配 9 个 MCP，各被要求使用其中 2 个（共 6 个），另外 3 个从不使用；'
            f'MCP 启动超时设为 60 s。(a) 会话期间 MCP 相关内存的峰值（原生：会话下的 MCP 进程树；MCPcell：转发程序加守护进程及其实例）；'
            f'(b) 会话下的进程构成，均值；(c) 成功调用到的被要求 MCP 数，原生方式下 npx 启动的 MCP 在模型查找工具时都还没就绪。'
            f'均值 ± 标准差，n = {n} 轮，点为单轮结果。')


def hbars(ax, groups, fmt, note, title):
    """Native vs MCPcell per scenario, values written on the bars."""
    h = 0.36
    top = max(g[1] for g in groups)
    for i, (name, nat, cel) in enumerate(groups):
        y = len(groups) - 1 - i
        ax.barh(y + h / 2, nat, h, color=NATIVE, label='原生' if i == 0 else None)
        ax.barh(y - h / 2, cel, h, color=CELL, label='MCPcell' if i == 0 else None)
        ax.text(nat + top * 0.015, y + h / 2, fmt(nat), va='center', fontsize=8, color=NATIVE)
        ax.text(cel + top * 0.015, y - h / 2, fmt(cel), va='center', fontsize=8, color=CELL, fontweight='bold')
        ax.text(top * 1.33, y, note(nat, cel), va='center', ha='center', fontsize=10, fontweight='bold', color='#222222')
    ax.set_yticks(range(len(groups)), [g[0] for g in groups][::-1])
    ax.set_xlim(0, top * 1.48)
    ax.set_xticks([])
    ax.spines['bottom'].set_visible(False)
    ax.spines['left'].set_visible(False)
    ax.tick_params(axis='y', length=0, labelsize=8.5)
    ax.grid(False)
    ax.set_title(title, loc='left', fontsize=10, fontweight='bold', pad=8)
    ax.legend(loc='lower right', bbox_to_anchor=(1.0, 1.0), ncol=2, fontsize=8, handlelength=1.2, borderaxespad=0.2)


def summary(bench, readiness, codex, out):
    """Two plain figures: memory and waiting time, native vs MCPcell."""
    rows = bench['scaling']
    mean = lambda vals: float(np.mean(vals))
    mb = lambda n, mode: mean([r['mb'] for r in rows if r['n'] == n and r['mode'] == mode])
    ready = lambda n, mode: mean([r['allReady'] for r in rows if r['n'] == n and r['mode'] == mode])
    memory = [('1 个会话\n（9 个 MCP）', mb(1, 'native'), mb(1, 'mcpcell')), ('4 个会话\n（9 个 MCP）', mb(4, 'native'), mb(4, 'mcpcell'))]
    if codex:
        peak = lambda m: mean([r['peakSum'] for r in codex if r['mode'] == m])
        memory.append(('Codex 实际使用\n（3 个会话）', peak('native'), peak('wrap')))
    one = readiness or [r for r in rows if r['n'] == 1]
    local = ('sequential-thinking', 'memory', 'filesystem')
    first = lambda v: float(np.median([t for r in bench['latency'] if r['server'] in local and r['variant'] == v for t in r['first']]))
    speed = [('所有 MCP 可用\n（1 个会话）', mean([r['allReady'] for r in one if r['mode'] == 'native']),
              mean([r['allReady'] for r in one if r['mode'] == 'mcpcell'])),
             ('所有 MCP 可用\n（4 个会话）', ready(4, 'native'), ready(4, 'mcpcell')),
             ('第一次调用\n拿到结果', first('native'), first('mcpcell-cold'))]
    size = lambda v: f'{v / 1024:.1f} GB' if v >= 1024 else f'{v:.0f} MB'
    secs = lambda v: f'{v:.1f} 秒' if v >= 1 else f'{v:.2f} 秒'
    saved = lambda a, b: f'省 {(1 - b / a) * 100:.0f}%'
    faster = lambda a, b: f'快 {a / b:.0f} 倍'
    for name, groups, fmt, note, title in (('memory', memory, size, saved, '内存占用（越少越好）'),
                                           ('speed', speed, secs, faster, '等待时间（越短越好）')):
        fig, ax = plt.subplots(figsize=(5.6, 0.75 + 0.8 * len(groups)), constrained_layout=True)
        hbars(ax, groups, fmt, note, title)
        save(fig, out, name)
    fig, (a, b) = plt.subplots(1, 2, figsize=(DOUBLE * 1.35, 3.2), constrained_layout=True)
    hbars(a, memory, size, saved, '内存占用（越少越好）')
    hbars(b, speed, secs, faster, '等待时间（越短越好）')
    save(fig, out, 'memory_speed')
    warm = float(np.median([t for r in bench['latency'] if r['server'] in local and r['variant'] == 'mcpcell-warm' for t in r['first']]))
    return ('**内存占用**：会话里所有 MCP 相关进程的私有提交（MCPcell 含守护进程）。前两组为脚本模拟的空闲会话（已连接、未调用），每组 3 次取平均；'
            'Codex 组为 3 个 Codex 会话并发、各用其中 2 个 MCP 的峰值，3 轮取平均。\n\n'
            f'**等待时间**：“所有 MCP 可用”是从启动到每个 MCP 都能列出工具；“第一次调用拿到结果”是 memory、filesystem、sequential-thinking '
            f'从启动到第一个工具结果的中位数，MCPcell 按最慢的情况算（实例还没运行，要先启动）；如果别的会话已经启动了共享实例，只要 {warm:.2f} 秒。'
            '经 MCPcell 的每次调用多一次本机转发，本地工具多 0.3–0.5 毫秒。')


def main():
    args = sys.argv[1:]
    bench = json.load(open(args[0], encoding='utf-8'))
    out = args[1]
    os.makedirs(out, exist_ok=True)
    readiness, codex, requested = None, [], {}
    rest = args[2:]
    while rest:
        flag = rest.pop(0)
        if flag == '--readiness':
            readiness = json.load(open(rest.pop(0), encoding='utf-8'))['readiness']
        elif flag == '--codex':
            requested = json.load(open(rest.pop(0), encoding='utf-8'))
            while rest and not rest[0].startswith('--'):
                codex.append(json.load(open(rest.pop(0), encoding='utf-8')))
    open(os.path.join(out, 'captions.md'), 'w', encoding='utf-8').write(summary(bench, readiness, codex, out) + '\n')
    detail = os.path.join(out, 'detail')
    os.makedirs(detail, exist_ok=True)
    captions = [fig_scaling(bench, detail),
                fig_readiness(readiness or [r for r in bench['scaling'] if r['n'] == 1], bench['servers'], detail),
                fig_latency(bench, detail)]
    if codex:
        captions.append(fig_codex(codex, requested, detail))
    open(os.path.join(detail, 'captions.md'), 'w', encoding='utf-8').write('\n\n'.join(captions) + '\n')
    print('wrote memory, speed and memory_speed to', out, '; detail figures to', detail)


if __name__ == '__main__':
    main()
