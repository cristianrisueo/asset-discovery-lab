//! `explorer healthcheck`: GET /healthz on localhost and exit 0 or 1 (spec
//! §13.8). The slim runtime image has no curl, so compose uses this.

use std::io::{Read, Write};
use std::net::{Ipv4Addr, SocketAddr, TcpStream};
use std::time::Duration;

const TIMEOUT: Duration = Duration::from_secs(2);

/// Returns Ok when `/healthz` on `127.0.0.1:port` answers 200.
pub fn check(port: u16) -> Result<(), String> {
    let addr = SocketAddr::from((Ipv4Addr::LOCALHOST, port));
    let mut s = TcpStream::connect_timeout(&addr, TIMEOUT).map_err(|e| format!("{addr}: {e}"))?;
    s.set_read_timeout(Some(TIMEOUT))
        .map_err(|e| e.to_string())?;
    s.set_write_timeout(Some(TIMEOUT))
        .map_err(|e| e.to_string())?;
    s.write_all(b"GET /healthz HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
        .map_err(|e| e.to_string())?;
    let mut buf = Vec::new();
    s.take(4096)
        .read_to_end(&mut buf)
        .map_err(|e| e.to_string())?;
    let head = String::from_utf8_lossy(&buf);
    let status_line = head.lines().next().unwrap_or_default();
    if is_ok(status_line) {
        Ok(())
    } else {
        Err(format!("unexpected answer: {status_line:?}"))
    }
}

fn is_ok(status_line: &str) -> bool {
    let mut parts = status_line.split_whitespace();
    matches!(parts.next(), Some(v) if v.starts_with("HTTP/")) && parts.next() == Some("200")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn status_lines() {
        assert!(is_ok("HTTP/1.1 200 OK"));
        assert!(!is_ok("HTTP/1.1 503 Service Unavailable"));
        assert!(!is_ok(""));
        assert!(!is_ok("200 OK"));
    }

    #[test]
    fn closed_port_fails() {
        // Bind and drop to get a port that is almost certainly closed.
        let port = std::net::TcpListener::bind("127.0.0.1:0")
            .unwrap()
            .local_addr()
            .unwrap()
            .port();
        assert!(check(port).is_err());
    }
}
