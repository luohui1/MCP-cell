import { useEffect, useState } from 'react';
import { Settings as SettingsIcon } from 'lucide-react';
import { takeover, type AutostartStatus } from '../api';
import { useStatus } from '../App';
import { masked } from '../clients';
import { bytes, since } from '../format';
import { CodeBlock, CopyButton, Dot, PageHeader, Section, Switch, useToast } from '../ui';
import { Row, Rows } from './ServiceDetail';

const commands = [
  'mcpcell start           # 在后台启动守护进程（无窗口）',
  'mcpcell stop            # 停止守护进程',
  'mcpcell ps              # 服务、实例、会话与内存',
  'mcpcell ui              # 打开本管理页',
  'mcpcell clients         # 各客户端的 MCP 条目与接管状态',
  'mcpcell restore --all   # 还原所有被接管的客户端配置（不需要守护进程）',
  'mcpcell autostart on    # 开机自启',
  'mcpcell update <服务>   # 更新 npx 服务的 npm 包',
].join('\n');

export function SettingsPage() {
  const { status } = useStatus();
  const toast = useToast();
  const [auto, setAuto] = useState<AutostartStatus | null>(null);
  useEffect(() => {
    takeover.autostartStatus().then(setAuto).catch(() => setAuto(null));
  }, []);
  async function toggle(on: boolean) {
    try {
      setAuto(await takeover.autostart(on));
      toast(on ? '已开启开机自启' : '已关闭开机自启');
    } catch (e) {
      toast((e as Error).message, 'error');
    }
  }
  return (
    <>
      <PageHeader icon={<SettingsIcon className="size-6 text-zinc-700 dark:text-zinc-300" />} title="设置" />
      <div className="mx-auto w-full max-w-[1000px] space-y-5 px-8 py-6">
        <Section title="守护进程">
          <Rows>
            <Row label="状态">
              <span className="inline-flex items-center gap-2">
                <Dot tone="green" />
                运行中 · 已运行 {since(status.startedAt)}
              </span>
            </Row>
            <Row label="版本">v{status.version}</Row>
            <Row label="监听地址">
              <span className="font-mono">127.0.0.1:{status.port}</span>（只接受本机连接）
            </Row>
            <Row label="进程">
              PID {status.pid} · 私有提交 {bytes(status.daemon.privateBytes)} · 工作集 {bytes(status.daemon.workingSetBytes)}
            </Row>
            <Row label="实例上限">同时最多 {status.maxInstances} 个服务实例；满了先回收最久未用的空闲实例</Row>
          </Rows>
        </Section>

        <Section title="开机自启">
          <div className="flex items-center gap-4">
            <div className="flex-1 text-sm leading-6">
              {auto?.supported === false ? (
                <span className="text-zinc-500">{auto.error}</span>
              ) : (
                <>
                  登录 Windows 时在后台启动 MCPcell（无窗口，日志写到 ~/.mcpcell/daemon.log）。接管客户端后建议开启，否则守护进程没运行时这些 MCP 不可用。
                  {auto?.enabled && auto.command && <div className="mt-1 truncate font-mono text-xs text-zinc-400">{auto.command}</div>}
                </>
              )}
            </div>
            {auto?.supported && <Switch checked={auto.enabled} onChange={toggle} label="开机自启" />}
          </div>
        </Section>

        <Section title="访问令牌">
          <div className="flex items-center gap-2">
            <code className="rounded-lg bg-zinc-100 px-3 py-2 font-mono text-[13px] dark:bg-zinc-800">{masked(status.token)}</code>
            <CopyButton text={status.token} label="复制访问令牌" />
          </div>
          <p className="mt-3 text-sm leading-6 text-zinc-500">
            客户端配置里的 <span className="font-mono">Authorization: Bearer …</span> 就是它。保存在本机的 MCPcell 目录里，不要分享。
          </p>
        </Section>

        <Section title="配置文件">
          <div className="flex items-center gap-2">
            <code className="min-w-0 truncate rounded-lg bg-zinc-100 px-3 py-2 font-mono text-[13px] dark:bg-zinc-800">{status.configPath}</code>
            <CopyButton text={status.configPath} label="复制路径" />
          </div>
          <p className="mt-3 text-sm leading-6 text-zinc-500">在本页添加、编辑、删除服务会直接写入这个文件并立即生效。手动修改文件后需要重启守护进程。</p>
        </Section>

        <Section title="命令行">
          <CodeBlock shown={commands} copy={commands} />
        </Section>
      </div>
    </>
  );
}
