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

use std::env;
use std::fs;
use std::io::{self, BufRead, BufReader, Write};
use std::net::{Shutdown, SocketAddr, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{exit, Command, Stdio};
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
            LINE_CONTROL if trim(&line[1..]) == b"direct" => return run_direct(argv, Some(first)),
            LINE_STDERR => {
                let _ = io::stderr().write_all(&line[1..]);
            }
            _ => {
                write_stdout(&line);
                break; // the reply to initialize: the daemon has the session
            }
        }
    }
    let mut to_daemon = match conn.try_clone() {
        Ok(c) => c,
        Err(_) => return 1,
    };
    thread::spawn(move || {
        let _ = io::copy(&mut io::stdin(), &mut to_daemon);
        // The agent is done: end the session like a server's stdin closing.
        let _ = to_daemon.shutdown(Shutdown::Write);
    });
    loop {
        line.clear();
        if reader.read_until(b'\n', &mut line).unwrap_or(0) == 0 {
            return 0;
        }
        match line[0] {
            LINE_STDERR => {
                let _ = io::stderr().write_all(&line[1..]);
            }
            LINE_CONTROL => {}
            _ => {
                if !write_stdout(&line) {
                    let _ = conn.shutdown(Shutdown::Both);
                    return 0; // the agent stopped reading
                }
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
    fn json() {
        let mut s = String::new();
        push_json(&mut s, r#"a"b\c"#);
        push_json(&mut s, "d\n\u{1}é");
        assert_eq!(s, r#""a\"b\\c""d\n\u0001é""#);
        assert_eq!(json_number("{\n  \"pid\": 1,\n  \"port\": 47621,\n}", "port"), Some(47621));
        assert_eq!(json_number("{}", "port"), None);
    }
}
