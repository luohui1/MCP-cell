//! mcpcell-run is what an agent's MCP entry launches in place of the server:
//!
//!     mcpcell-run [--shared | --per-session | --direct] -- <command> [args...]
//!
//! The entry keeps the real command, so the agent's configuration still says
//! exactly which server runs; removing the prefix is the native setup again.
//! mcpcell-run hands the session to the MCPcell daemon (starting it when
//! needed) and moves bytes between the agent and the daemon. Whenever MCPcell
//! cannot serve it, it runs the command itself, as the agent would have.
//!
//! One runs per agent session per server, so it is written to stay tiny:
//! standard library only, no console of its own on Windows. The wire format
//! is described in internal/httpd/run.go.
#![windows_subsystem = "windows"]

use std::collections::HashSet;
use std::env;
use std::fs;
use std::io::{self, BufRead, BufReader, Write};
use std::net::{Shutdown, SocketAddr, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{self, exit, Child, ChildStdin, ChildStdout, Command, Stdio};
use std::sync::{Arc, Condvar, Mutex};
use std::thread;
use std::time::{Duration, Instant};

const LINE_STDERR: u8 = 0x1e; // the rest of the line is for the agent's stderr
const LINE_CONTROL: u8 = 0x1f; // an instruction from the daemon

fn main() {
    let args: Vec<String> = env::args().skip(1).collect();
    let (mode, argv) = parse_args(&args);
    if argv.is_empty() {
        note_raw("用法: mcpcell-run [--shared | --per-session | --direct] -- <命令> [参数...]");
        exit(2);
    }
    if mode == "direct" {
        exit(run_direct(&argv, None));
    }
    match connect() {
        Ok((conn, reader)) => exit(serve(conn, reader, &argv, &mode)),
        Err(e) => {
            note(&format!("MCPcell 守护进程不可用（{e}），直接运行原命令"));
            exit(run_direct(&argv, None));
        }
    }
}

fn parse_args(args: &[String]) -> (String, Vec<String>) {
    let mut mode = "auto".to_string();
    for (i, a) in args.iter().enumerate() {
        match a.as_str() {
            "--" => return (mode, args[i + 1..].to_vec()),
            "--shared" => mode = "shared".into(),
            "--per-session" => mode = "per-session".into(),
            "--direct" => mode = "direct".into(),
            "--auto" => mode = "auto".into(),
            _ => return (mode, args[i..].to_vec()), // no "--": the command starts here
        }
    }
    (mode, Vec::new())
}

fn note(text: &str) {
    note_raw(&format!("[mcpcell] {text}"));
}

fn note_raw(text: &str) {
    let _ = writeln!(io::stderr(), "{text}");
}

/// Runs the session through the daemon. Until the daemon answers the agent's
/// initialize it may still hand the session back ("direct"); the initialize
/// request is then replayed to the command run directly.
fn serve(mut conn: TcpStream, mut reader: BufReader<TcpStream>, argv: &[String], mode: &str) -> i32 {
    let mut first = Vec::new();
    if io::stdin().lock().read_until(b'\n', &mut first).unwrap_or(0) == 0 {
        return 0; // the agent closed stdin before saying anything
    }
    let mut header = header_json(argv, mode).into_bytes();
    header.push(b'\n');
    header.extend_from_slice(&first);
    if conn.write_all(&header).is_err() {
        note("无法连接 MCPcell，直接运行原命令");
        return run_direct(argv, Some(first));
    }
    let mut line = Vec::new();
    loop {
        line.clear();
        let n = reader.read_until(b'\n', &mut line).unwrap_or(0);
        if n == 0 {
            note("MCPcell 没有应答，直接运行原命令");
            return run_direct(argv, Some(first));
        }
        match line[0] {
            // "direct", or a daemon on its way out: nothing was answered yet.
            LINE_CONTROL => return run_direct(argv, Some(first)),
            LINE_STDERR => {
                let _ = io::stderr().write_all(&line[1..]);
            }
            _ => {
                write_stdout(&line);
                break; // the reply to initialize: the daemon has the session
            }
        }
    }
    let session = Session { argv: argv.to_vec(), mode: mode.to_string(), first, shared: Arc::default() };
    match conn.try_clone() {
        Ok(sink) => session.attach(Sink::Tcp(sink)),
        Err(_) => return 1,
    }
    let shared = session.shared.clone();
    thread::spawn(move || pump_stdin(&shared));
    session.run(Source::Daemon(reader))
}

/// One agent session, kept alive across the backends that serve it: the
/// daemon, a reconnected daemon, or the command run directly.
struct Session {
    argv: Vec<String>,
    mode: String,
    /// The agent's initialize request, replayed to every new backend.
    first: Vec<u8>,
    shared: Arc<Shared>,
}

#[derive(Default)]
struct Shared {
    state: Mutex<State>,
    ready: Condvar,
}

#[derive(Default)]
struct State {
    /// Where the agent's messages go; None while a new backend is attached.
    sink: Option<Sink>,
    /// Ids of the agent's requests not answered yet.
    inflight: HashSet<Vec<u8>>,
    agent_done: bool,
    over: bool,
}

enum Sink {
    Tcp(TcpStream),
    Child(Option<ChildStdin>),
}

impl Sink {
    fn send(&mut self, line: &[u8]) -> io::Result<()> {
        match self {
            Sink::Tcp(c) => c.write_all(line),
            Sink::Child(Some(c)) => c.write_all(line).and_then(|_| c.flush()),
            Sink::Child(None) => Err(io::ErrorKind::BrokenPipe.into()),
        }
    }

    /// Ends the session the way a server's stdin closing does.
    fn close(&mut self) {
        match self {
            Sink::Tcp(c) => {
                let _ = c.shutdown(Shutdown::Write);
            }
            Sink::Child(c) => *c = None,
        }
    }
}

enum Source {
    Daemon(BufReader<TcpStream>),
    Child(BufReader<ChildStdout>, Child),
}

const INITIALIZED: &[u8] = b"{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n";

/// Moves the agent's messages to whichever backend is attached, noting the
/// requests so they can be answered if the backend goes away first.
fn pump_stdin(shared: &Shared) {
    let stdin = io::stdin();
    let mut input = stdin.lock();
    let mut line = Vec::new();
    loop {
        line.clear();
        let n = input.read_until(b'\n', &mut line).unwrap_or(0);
        let mut st = shared.state.lock().unwrap();
        if n == 0 {
            st.agent_done = true;
            if let Some(sink) = st.sink.as_mut() {
                sink.close();
            }
            shared.ready.notify_all();
            return;
        }
        while st.sink.is_none() && !st.over {
            st = shared.ready.wait(st).unwrap();
        }
        if st.over {
            return;
        }
        if let Kind::Request(id) = classify(&line) {
            st.inflight.insert(id);
        }
        // A broken backend shows up on the reading side.
        let _ = st.sink.as_mut().unwrap().send(&line);
    }
}

impl Session {
    fn attach(&self, sink: Sink) {
        let mut st = self.shared.state.lock().unwrap();
        st.sink = Some(sink);
        self.shared.ready.notify_all();
    }

    /// Answers what the agent sent to the lost backend and holds back what
    /// it sends next until another backend is attached.
    fn detach(&self) {
        let ids: Vec<Vec<u8>> = {
            let mut st = self.shared.state.lock().unwrap();
            st.sink = None;
            st.inflight.drain().collect()
        };
        for id in &ids {
            write_stdout(&unknown_result(id));
        }
        if !ids.is_empty() {
            note(&format!("{} 个进行中的调用结果未知，未自动重试", ids.len()));
        }
    }

    fn end(&self) {
        let mut st = self.shared.state.lock().unwrap();
        st.over = true;
        self.shared.ready.notify_all();
    }

    fn agent_done(&self) -> bool {
        self.shared.state.lock().unwrap().agent_done
    }

    /// Relays the backend to the agent until the session ends, replacing the
    /// backend when it goes away.
    fn run(&self, mut src: Source) -> i32 {
        let mut line = Vec::new();
        let mut leaving: Option<Vec<u8>> = None;
        loop {
            line.clear();
            let n = match &mut src {
                Source::Daemon(r) => r.read_until(b'\n', &mut line),
                Source::Child(r, _) => r.read_until(b'\n', &mut line),
            }
            .unwrap_or(0);
            if n == 0 {
                if self.agent_done() {
                    return match &mut src {
                        Source::Child(_, child) => child.wait().ok().and_then(|s| s.code()).unwrap_or(0),
                        Source::Daemon(_) => 0,
                    };
                }
                self.detach();
                let next = match src {
                    Source::Daemon(_) => self.replace_daemon(leaving.take()),
                    Source::Child(_, mut child) => {
                        // The server itself exited, as it would have natively.
                        self.end();
                        return child.wait().ok().and_then(|s| s.code()).unwrap_or(1);
                    }
                };
                match next {
                    Some(s) => src = s,
                    None => {
                        self.end();
                        return 1;
                    }
                }
                continue;
            }
            if matches!(src, Source::Daemon(_)) {
                match line[0] {
                    LINE_STDERR => {
                        let _ = io::stderr().write_all(&line[1..]);
                        continue;
                    }
                    LINE_CONTROL => {
                        leaving = Some(trim(&line[1..]).to_vec());
                        continue;
                    }
                    _ => {}
                }
            }
            if let Kind::Response(id, _) = classify(&line) {
                self.shared.state.lock().unwrap().inflight.remove(&id);
            }
            if !write_stdout(&line) {
                self.end();
                return 0; // the agent stopped reading
            }
        }
    }

    /// Finds the next backend after the daemon went away: the daemon again
    /// unless MCPcell is quitting, otherwise the command run directly.
    fn replace_daemon(&self, leaving: Option<Vec<u8>>) -> Option<Source> {
        if leaving.as_deref() != Some(b"stop") {
            let restarting = leaving.as_deref() == Some(b"restart");
            note(if restarting { "MCPcell 正在更新，等待它重新启动" } else { "与 MCPcell 的连接断开，正在重连" });
            if let Some((conn, reader)) = reconnect(restarting) {
                match self.attach_daemon(conn, reader) {
                    Ok(src) => {
                        note("已重新连上 MCPcell");
                        return Some(src);
                    }
                    Err(e) => note(&format!("重新连上后无法恢复会话（{e}）")),
                }
            }
        }
        note("改为直接运行原命令");
        match self.attach_child() {
            Ok(src) => Some(src),
            Err(e) => {
                note(&format!("无法直接运行 {}：{e}", self.argv[0]));
                None
            }
        }
    }

    /// Opens the session again on a new daemon connection: the agent's
    /// initialize is replayed and its answer kept from the agent, which has
    /// one already.
    fn attach_daemon(&self, mut conn: TcpStream, mut reader: BufReader<TcpStream>) -> Result<Source, String> {
        let mut header = header_json(&self.argv, &self.mode).into_bytes();
        header.push(b'\n');
        header.extend_from_slice(&self.first);
        conn.write_all(&header).map_err(|e| e.to_string())?;
        let _ = reader.get_ref().set_read_timeout(Some(REPLAY_TIMEOUT));
        let mut line = Vec::new();
        loop {
            line.clear();
            if reader.read_until(b'\n', &mut line).map_err(|e| e.to_string())? == 0 {
                return Err("连接又断开了".into());
            }
            match line[0] {
                LINE_STDERR => {} // the notice the agent has seen already
                LINE_CONTROL => return Err(format!("守护进程不接手（{}）", String::from_utf8_lossy(trim(&line[1..])))),
                _ => match classify(&line) {
                    Kind::Response(_, false) => break,
                    Kind::Response(_, true) => return Err("初始化被拒绝".into()),
                    _ => {}
                },
            }
        }
        let _ = reader.get_ref().set_read_timeout(None);
        conn.write_all(INITIALIZED).map_err(|e| e.to_string())?;
        self.attach(Sink::Tcp(conn));
        Ok(Source::Daemon(reader))
    }

    /// Runs the command directly and replays the agent's initialize to it, as
    /// attach_daemon does for a daemon.
    fn attach_child(&self) -> Result<Source, String> {
        let mut child = Command::new(resolve(&self.argv[0]))
            .args(&self.argv[1..])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .spawn()
            .map_err(|e| e.to_string())?;
        let (mut input, output) = (child.stdin.take().unwrap(), child.stdout.take().unwrap());
        let mut reader = BufReader::new(output);
        input.write_all(&self.first).map_err(|e| e.to_string())?;
        let mut line = Vec::new();
        loop {
            line.clear();
            if reader.read_until(b'\n', &mut line).map_err(|e| e.to_string())? == 0 {
                return Err("进程启动后就退出了".into());
            }
            match classify(&line) {
                Kind::Response(_, false) => break,
                Kind::Response(_, true) => return Err("初始化被拒绝".into()),
                _ => {}
            }
        }
        input.write_all(INITIALIZED).map_err(|e| e.to_string())?;
        self.attach(Sink::Child(Some(input)));
        Ok(Source::Child(reader, child))
    }
}

/// How long a replayed initialize may take: a server that is not running
/// yet has to start first.
const REPLAY_TIMEOUT: Duration = Duration::from_secs(180);

/// The JSON-RPC error MCPcell gives for a call whose result is unknown
/// (jsonrpc.CodeResultUnknown).
fn unknown_result(id: &[u8]) -> Vec<u8> {
    let mut out = b"{\"jsonrpc\":\"2.0\",\"id\":".to_vec();
    out.extend_from_slice(id);
    out.extend_from_slice(
        ",\"error\":{\"code\":-32002,\"message\":\"与 MCP 服务的连接中断，调用结果未知，未自动重试\"}}\n".as_bytes(),
    );
    out
}

/// Reaches a daemon again after the connection dropped. A daemon being
/// replaced comes back by itself; one that crashed is started again, by one
/// wrapper (the others find it running).
fn reconnect(restarting: bool) -> Option<(TcpStream, BufReader<TcpStream>)> {
    let began = Instant::now();
    let start_after = if restarting { Duration::from_secs(20) } else { Duration::from_millis(u64::from(process::id() % 1000)) };
    let mut started = false;
    loop {
        match dial() {
            Ok(c) => return Some(c),
            Err(DialError::Refused(_)) => return None,
            Err(DialError::Down(_)) => {}
        }
        let waited = began.elapsed();
        if waited > Duration::from_secs(40) {
            return None;
        }
        if !started && waited >= start_after {
            started = start_daemon().is_ok();
        }
        thread::sleep(Duration::from_millis(250));
    }
}

/// What a JSON-RPC line is, as far as the wrapper cares.
#[derive(Debug, PartialEq)]
enum Kind {
    /// A request and its raw id.
    Request(Vec<u8>),
    /// A response, its raw id and whether it is an error.
    Response(Vec<u8>, bool),
    Other,
}

/// Reads the top level of one JSON object: just enough to tell requests from
/// responses and keep their ids as written.
fn classify(line: &[u8]) -> Kind {
    let mut p = Scan { s: line, i: 0 };
    p.ws();
    if !p.eat(b'{') {
        return Kind::Other;
    }
    let (mut id, mut method, mut error) = (None, false, false);
    loop {
        p.ws();
        if p.eat(b'}') {
            break;
        }
        let Some(key) = p.string() else { return Kind::Other };
        p.ws();
        if !p.eat(b':') {
            return Kind::Other;
        }
        p.ws();
        let start = p.i;
        if !p.value() {
            return Kind::Other;
        }
        match key {
            b"\"id\"" => id = Some(line[start..p.i].to_vec()),
            b"\"method\"" => method = true,
            b"\"error\"" => error = true,
            _ => {}
        }
        p.ws();
        if p.eat(b',') {
            continue;
        }
        if p.eat(b'}') {
            break;
        }
        return Kind::Other;
    }
    match (id, method) {
        (Some(id), true) if id != b"null" => Kind::Request(id),
        (Some(id), false) => Kind::Response(id, error),
        _ => Kind::Other,
    }
}

struct Scan<'a> {
    s: &'a [u8],
    i: usize,
}

impl<'a> Scan<'a> {
    fn ws(&mut self) {
        while self.i < self.s.len() && self.s[self.i].is_ascii_whitespace() {
            self.i += 1;
        }
    }

    fn eat(&mut self, c: u8) -> bool {
        if self.s.get(self.i) == Some(&c) {
            self.i += 1;
            return true;
        }
        false
    }

    /// A string, quotes included.
    fn string(&mut self) -> Option<&'a [u8]> {
        let start = self.i;
        if !self.eat(b'"') {
            return None;
        }
        while self.i < self.s.len() {
            match self.s[self.i] {
                b'\\' => self.i += 2,
                b'"' => {
                    self.i += 1;
                    let s: &'a [u8] = self.s;
                    return Some(&s[start..self.i]);
                }
                _ => self.i += 1,
            }
        }
        None
    }

    fn value(&mut self) -> bool {
        match self.s.get(self.i) {
            None => false,
            Some(b'"') => self.string().is_some(),
            Some(b'{') | Some(b'[') => {
                let mut depth = 0usize;
                while self.i < self.s.len() {
                    match self.s[self.i] {
                        b'"' => {
                            if self.string().is_none() {
                                return false;
                            }
                            continue;
                        }
                        b'{' | b'[' => depth += 1,
                        b'}' | b']' => {
                            depth -= 1;
                            if depth == 0 {
                                self.i += 1;
                                return true;
                            }
                        }
                        _ => {}
                    }
                    self.i += 1;
                }
                false
            }
            Some(_) => {
                let start = self.i;
                while self.i < self.s.len() && !matches!(self.s[self.i], b',' | b'}' | b']') && !self.s[self.i].is_ascii_whitespace() {
                    self.i += 1;
                }
                self.i > start
            }
        }
    }
}

fn write_stdout(line: &[u8]) -> bool {
    let mut out = io::stdout().lock();
    out.write_all(line).is_ok() && out.flush().is_ok()
}

fn trim(b: &[u8]) -> &[u8] {
    let s = b.iter().position(|c| !c.is_ascii_whitespace()).unwrap_or(b.len());
    let e = b.iter().rposition(|c| !c.is_ascii_whitespace()).map_or(s, |i| i + 1);
    &b[s..e]
}

/// Runs the command exactly as the agent would have: its environment,
/// directory and stdio. replay is what the agent already sent.
fn run_direct(argv: &[String], replay: Option<Vec<u8>>) -> i32 {
    let mut cmd = Command::new(resolve(&argv[0]));
    cmd.args(&argv[1..]).stdout(Stdio::inherit()).stderr(Stdio::inherit());
    cmd.stdin(if replay.is_some() { Stdio::piped() } else { Stdio::inherit() });
    let mut child = match cmd.spawn() {
        Ok(c) => c,
        Err(e) => {
            note(&format!("无法运行 {}：{e}", argv[0]));
            return 1;
        }
    };
    if let (Some(first), Some(mut input)) = (replay, child.stdin.take()) {
        thread::spawn(move || {
            if input.write_all(&first).is_ok() {
                let _ = io::copy(&mut io::stdin(), &mut input);
            }
        });
    }
    child.wait().ok().and_then(|s| s.code()).unwrap_or(1)
}

/// Finds a bare command on PATH the way a shell would on Windows, trying each
/// PATHEXT extension (`npx` is `npx.cmd`).
fn resolve(name: &str) -> PathBuf {
    let p = Path::new(name);
    if !cfg!(windows) || p.extension().is_some() || p.components().count() > 1 {
        return p.to_path_buf();
    }
    let exts = env::var("PATHEXT").unwrap_or_else(|_| ".COM;.EXE;.BAT;.CMD".into());
    let dirs = env::var_os("PATH").unwrap_or_default();
    for dir in env::split_paths(&dirs) {
        for ext in exts.split(';').filter(|e| !e.is_empty()) {
            let candidate = dir.join(format!("{name}{}", ext.to_ascii_lowercase()));
            if candidate.is_file() {
                return candidate;
            }
        }
    }
    p.to_path_buf()
}

fn header_json(argv: &[String], mode: &str) -> String {
    let cwd = env::current_dir().map(|d| d.to_string_lossy().into_owned()).unwrap_or_default();
    let mut s = String::from("{\"argv\":[");
    for (i, a) in argv.iter().enumerate() {
        if i > 0 {
            s.push(',');
        }
        push_json(&mut s, a);
    }
    s.push_str("],\"cwd\":");
    push_json(&mut s, &cwd);
    s.push_str(",\"mode\":");
    push_json(&mut s, mode);
    s.push_str(",\"env\":{");
    for (i, (k, v)) in env::vars_os().enumerate() {
        if i > 0 {
            s.push(',');
        }
        push_json(&mut s, &k.to_string_lossy());
        s.push(':');
        push_json(&mut s, &v.to_string_lossy());
    }
    s.push_str("}}");
    s
}

fn push_json(out: &mut String, v: &str) {
    out.push('"');
    for c in v.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
}

fn home() -> PathBuf {
    if let Some(h) = env::var_os("MCPCELL_HOME") {
        return PathBuf::from(h);
    }
    let user = env::var_os("USERPROFILE").or_else(|| env::var_os("HOME")).unwrap_or_default();
    PathBuf::from(user).join(".mcpcell")
}

enum DialError {
    /// Nothing is listening: starting the daemon may help.
    Down(String),
    /// A daemon answered but will not serve this (too old, wrong token):
    /// starting another one cannot help.
    Refused(String),
}

/// Reaches the daemon, starting it once if it is not running.
fn connect() -> Result<(TcpStream, BufReader<TcpStream>), String> {
    match dial() {
        Ok(c) => return Ok(c),
        Err(DialError::Refused(e)) => return Err(e),
        Err(DialError::Down(_)) => start_daemon()?,
    }
    let deadline = Instant::now() + Duration::from_secs(10);
    loop {
        match dial() {
            Ok(c) => return Ok(c),
            Err(DialError::Refused(e)) => return Err(e),
            Err(DialError::Down(e)) if Instant::now() >= deadline => return Err(e),
            Err(DialError::Down(_)) => thread::sleep(Duration::from_millis(200)),
        }
    }
}

fn dial() -> Result<(TcpStream, BufReader<TcpStream>), DialError> {
    let down = |e: String| DialError::Down(e);
    let info = fs::read_to_string(home().join("daemon.json")).map_err(|_| down("守护进程未运行".into()))?;
    let port = json_number(&info, "port").ok_or_else(|| down("daemon.json 无效".into()))?;
    let token = fs::read_to_string(home().join("token")).map_err(|_| down("读不到访问令牌".into()))?;
    let addr = SocketAddr::from(([127, 0, 0, 1], port));
    let mut conn = TcpStream::connect_timeout(&addr, Duration::from_secs(2)).map_err(|_| down(format!("连不上 {addr}")))?;
    let refused = |e: String| DialError::Refused(e);
    let _ = conn.set_read_timeout(Some(Duration::from_secs(5)));
    let req = format!(
        "GET /run HTTP/1.1\r\nHost: {addr}\r\nAuthorization: Bearer {}\r\nConnection: Upgrade\r\nUpgrade: mcpcell-run\r\n\r\n",
        token.trim()
    );
    conn.write_all(req.as_bytes()).map_err(|e| refused(e.to_string()))?;
    let mut reader = BufReader::new(conn.try_clone().map_err(|e| refused(e.to_string()))?);
    let mut status = String::new();
    reader.read_line(&mut status).map_err(|e| refused(e.to_string()))?;
    if status.contains(" 503 ") {
        return Err(down("守护进程正在退出".into())); // the next one will be along
    }
    if !status.contains(" 101 ") {
        return Err(refused(format!("守护进程拒绝了连接：{}", status.trim())));
    }
    loop {
        let mut h = String::new();
        if reader.read_line(&mut h).map_err(|e| refused(e.to_string()))? == 0 {
            return Err(refused("连接在握手时断开".into()));
        }
        if h.trim().is_empty() {
            break;
        }
    }
    // The reader reads through a duplicated socket, which on Windows keeps
    // its own copy of the timeout: clear both.
    let _ = conn.set_read_timeout(None);
    let _ = reader.get_ref().set_read_timeout(None);
    Ok((conn, reader))
}

/// Reads `"key": 123` out of a small JSON document.
fn json_number(doc: &str, key: &str) -> Option<u16> {
    let at = doc.find(&format!("\"{key}\""))?;
    let rest = &doc[at + key.len() + 2..];
    let rest = rest[rest.find(':')? + 1..].trim_start();
    let digits: String = rest.chars().take_while(|c| c.is_ascii_digit()).collect();
    digits.parse().ok()
}

/// Launches the daemon that sits next to this program.
fn start_daemon() -> Result<(), String> {
    let me = env::current_exe().map_err(|e| e.to_string())?;
    let name = if cfg!(windows) { "mcpcelld.exe" } else { "mcpcell" };
    let daemon = me.parent().map(|d| d.join(name)).ok_or("找不到守护进程")?;
    if !daemon.is_file() {
        return Err(format!("找不到 {}", daemon.display()));
    }
    let spawn = |breakaway: bool| {
        let mut cmd = Command::new(&daemon);
        cmd.args(["daemon", "--log"]).stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null());
        detach(&mut cmd, breakaway);
        cmd.spawn()
    };
    // Leaving the agent's job keeps the daemon alive when the agent exits; a
    // job that forbids it makes that start fail, so try again without.
    spawn(true).or_else(|_| spawn(false)).map(|_| ()).map_err(|e| e.to_string())
}

#[cfg(windows)]
fn detach(cmd: &mut Command, breakaway: bool) {
    use std::os::windows::process::CommandExt;
    // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP [| CREATE_BREAKAWAY_FROM_JOB]
    cmd.creation_flags(0x0000_0008 | 0x0000_0200 | if breakaway { 0x0100_0000 } else { 0 });
}

#[cfg(not(windows))]
fn detach(cmd: &mut Command, _breakaway: bool) {
    use std::os::unix::process::CommandExt;
    cmd.process_group(0);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn args() {
        let v = |s: &[&str]| s.iter().map(|x| x.to_string()).collect::<Vec<_>>();
        assert_eq!(parse_args(&v(&["--", "npx", "-y", "x"])), ("auto".into(), v(&["npx", "-y", "x"])));
        assert_eq!(parse_args(&v(&["--shared", "--", "npx"])), ("shared".into(), v(&["npx"])));
        assert_eq!(parse_args(&v(&["node", "--", "s.js"])), ("auto".into(), v(&["node", "--", "s.js"])));
        assert_eq!(parse_args(&v(&["--direct"])).1.len(), 0);
    }

    #[test]
    fn kinds() {
        let req = br#"{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"x","arguments":{"s":"}\"]","n":[1,{}]}}}"#;
        assert_eq!(classify(req), Kind::Request(b"7".to_vec()));
        assert_eq!(classify(br#"{ "id" : "a\"b" , "result" : {"content":[]} }"#), Kind::Response(br#""a\"b""#.to_vec(), false));
        assert_eq!(classify(br#"{"jsonrpc":"2.0","id":3,"error":{"code":-1,"message":"m"}}"#), Kind::Response(b"3".to_vec(), true));
        assert_eq!(classify(br#"{"jsonrpc":"2.0","method":"notifications/initialized"}"#), Kind::Other);
        assert_eq!(classify(br#"[{"id":1,"method":"x"}]"#), Kind::Other);
        assert_eq!(classify(b"not json"), Kind::Other);
        let err = unknown_result(br#""a\"b""#);
        assert_eq!(classify(&err), Kind::Response(br#""a\"b""#.to_vec(), true));
        assert!(err.ends_with(b"}}\n"));
    }

    #[test]
    fn json() {
        let mut s = String::new();
        push_json(&mut s, r#"a"b\c"#);
        push_json(&mut s, "d\n\u{1}é");
        assert_eq!(s, r#""a\"b\\c""d\n\u0001é""#);
        assert_eq!(json_number("{\n  \"pid\": 1,\n  \"port\": 47621,\n}", "port"), Some(47621));
        assert_eq!(json_number("{}", "port"), None);
    }
}
