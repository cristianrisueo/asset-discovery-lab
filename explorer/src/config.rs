//! Configuration from environment variables only (spec §13.7).

use std::time::Duration;

/// The Doris transport of the §3 R4 ladder.
// The variant names mirror the spec's transport values (§13.7).
#[allow(clippy::enum_variant_names)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Transport {
    /// Step 1: arrow-flight's Flight SQL client.
    FlightSql,
    /// Step 2: ADBC with the Flight SQL driver.
    AdbcFlightSql,
    /// Step 3: the MySQL protocol (a known deviation from the diagram).
    MySql,
}

impl Transport {
    /// The value reported as `doris_transport` in `/readyz`.
    pub fn name(self) -> &'static str {
        match self {
            Transport::FlightSql => "flight-sql",
            Transport::AdbcFlightSql => "adbc-flight-sql",
            Transport::MySql => "mysql",
        }
    }

    fn parse(s: &str) -> Result<Self, String> {
        match s {
            "flight-sql" => Ok(Transport::FlightSql),
            "adbc-flight-sql" => Ok(Transport::AdbcFlightSql),
            "mysql" => Ok(Transport::MySql),
            other => Err(format!(
                "DORIS_TRANSPORT={other:?}: want flight-sql, adbc-flight-sql or mysql"
            )),
        }
    }
}

/// The explorer's configuration.
#[derive(Debug, Clone)]
pub struct Config {
    pub listen_addr: String,
    pub doris_flight_addr: String,
    pub doris_user: String,
    pub doris_password: String,
    pub transport: Transport,
    pub doris_mysql_addr: String,
    pub lake_enabled: bool,
    pub query_timeout: Duration,
    pub rust_log: String,
}

impl Config {
    /// Reads the configuration from the process environment.
    pub fn from_env() -> Result<Self, String> {
        Self::from_lookup(|k| std::env::var(k).ok())
    }

    /// Reads the configuration through `get`, applying the §13.7 defaults.
    pub fn from_lookup(get: impl Fn(&str) -> Option<String>) -> Result<Self, String> {
        let var = |k: &str, def: &str| get(k).filter(|v| !v.is_empty()).unwrap_or(def.into());
        let lake = var("LAKE_ENABLED", "true");
        let lake_enabled = match lake.to_ascii_lowercase().as_str() {
            "true" | "1" | "yes" => true,
            "false" | "0" | "no" => false,
            other => return Err(format!("LAKE_ENABLED={other:?}: want true or false")),
        };
        let timeout_ms: u64 = var("QUERY_TIMEOUT_MS", "10000")
            .parse()
            .map_err(|e| format!("QUERY_TIMEOUT_MS: {e}"))?;
        if timeout_ms == 0 {
            return Err("QUERY_TIMEOUT_MS must be greater than 0".into());
        }
        Ok(Config {
            listen_addr: var("LISTEN_ADDR", "0.0.0.0:8080"),
            doris_flight_addr: var("DORIS_FLIGHT_ADDR", "http://doris-fe:8070"),
            doris_user: var("DORIS_USER", "explorer"),
            doris_password: get("DORIS_PASSWORD").unwrap_or_default(),
            transport: Transport::parse(&var("DORIS_TRANSPORT", "flight-sql"))?,
            doris_mysql_addr: var("DORIS_MYSQL_ADDR", "doris-fe:9030"),
            lake_enabled,
            query_timeout: Duration::from_millis(timeout_ms),
            rust_log: var("RUST_LOG", "info"),
        })
    }

    /// The port of `LISTEN_ADDR`, used by `explorer healthcheck`.
    pub fn listen_port(&self) -> Result<u16, String> {
        self.listen_addr
            .rsplit_once(':')
            .and_then(|(_, p)| p.parse().ok())
            .ok_or_else(|| format!("LISTEN_ADDR={:?} has no port", self.listen_addr))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    fn cfg(pairs: &[(&str, &str)]) -> Result<Config, String> {
        let m: HashMap<String, String> = pairs
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        Config::from_lookup(|k| m.get(k).cloned())
    }

    #[test]
    fn defaults_follow_the_spec() {
        let c = cfg(&[]).unwrap();
        assert_eq!(c.listen_addr, "0.0.0.0:8080");
        assert_eq!(c.doris_flight_addr, "http://doris-fe:8070");
        assert_eq!(c.doris_user, "explorer");
        assert_eq!(c.doris_password, "");
        assert_eq!(c.transport, Transport::FlightSql);
        assert_eq!(c.doris_mysql_addr, "doris-fe:9030");
        assert!(c.lake_enabled);
        assert_eq!(c.query_timeout, Duration::from_secs(10));
        assert_eq!(c.rust_log, "info");
        assert_eq!(c.listen_port().unwrap(), 8080);
    }

    #[test]
    fn overrides_and_errors() {
        let c = cfg(&[
            ("LAKE_ENABLED", "false"),
            ("DORIS_TRANSPORT", "mysql"),
            ("QUERY_TIMEOUT_MS", "2500"),
        ])
        .unwrap();
        assert!(!c.lake_enabled);
        assert_eq!(c.transport.name(), "mysql");
        assert_eq!(c.query_timeout, Duration::from_millis(2500));
        assert!(cfg(&[("DORIS_TRANSPORT", "jdbc")]).is_err());
        assert!(cfg(&[("LAKE_ENABLED", "maybe")]).is_err());
        assert!(cfg(&[("QUERY_TIMEOUT_MS", "0")]).is_err());
    }
}
