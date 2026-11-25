//! explorer: the HTTP JSON API of asset-discovery-lab (spec §13). It queries
//! Doris over Arrow Flight SQL and never exposes Doris to the browser.
//!
//! Subcommands: `serve` (default) and `healthcheck`.

mod api;
mod config;
mod convert;
mod error;
mod flight;
mod health;
mod logging;
mod service;
mod sql;
mod validate;

use std::process::ExitCode;
use std::sync::Arc;

use config::{Config, Transport};
use flight::{DorisQuery, FlightSql};
use service::Explorer;

fn main() -> ExitCode {
    let cfg = match Config::from_env() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("explorer: configuration: {e}");
            return ExitCode::from(2);
        }
    };
    match std::env::args().nth(1).as_deref() {
        None | Some("serve") => serve(cfg),
        Some("healthcheck") => match cfg.listen_port().and_then(health::check) {
            Ok(()) => ExitCode::SUCCESS,
            Err(e) => {
                eprintln!("explorer healthcheck: {e}");
                ExitCode::FAILURE
            }
        },
        Some(other) => {
            eprintln!("explorer: unknown subcommand {other:?} (want serve or healthcheck)");
            ExitCode::from(2)
        }
    }
}

fn serve(cfg: Config) -> ExitCode {
    logging::init(&cfg.rust_log);
    let rt = match tokio::runtime::Builder::new_multi_thread()
        .worker_threads(2)
        .enable_all()
        .build()
    {
        Ok(rt) => rt,
        Err(e) => {
            tracing::error!(error = %e, "cannot start the runtime");
            return ExitCode::FAILURE;
        }
    };
    match rt.block_on(run(cfg)) {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            tracing::error!(error = %e, "explorer stopped");
            ExitCode::FAILURE
        }
    }
}

async fn run(cfg: Config) -> Result<(), String> {
    let doris: Arc<dyn DorisQuery> = match cfg.transport {
        Transport::FlightSql => {
            let f = FlightSql::new(
                &cfg.doris_flight_addr,
                &cfg.doris_user,
                &cfg.doris_password,
                cfg.query_timeout,
            );
            // One authenticated client, created at start and reused (§13.6).
            if let Err(e) = f.connect().await {
                tracing::warn!(error = %e.message(), "Doris is not reachable yet; the next request retries");
            }
            f
        }
        other => {
            return Err(format!(
                "DORIS_TRANSPORT={} is a later step of the spec §3 R4 ladder and is not built; \
                 step 1 (flight-sql) works (decision D-040); DORIS_MYSQL_ADDR={} is unused",
                other.name(),
                cfg.doris_mysql_addr
            ));
        }
    };
    let explorer = Arc::new(Explorer::new(doris, cfg.lake_enabled));
    let listener = tokio::net::TcpListener::bind(&cfg.listen_addr)
        .await
        .map_err(|e| format!("listen on {}: {e}", cfg.listen_addr))?;
    tracing::info!(
        addr = %cfg.listen_addr,
        transport = cfg.transport.name(),
        doris = %cfg.doris_flight_addr,
        lake_enabled = cfg.lake_enabled,
        "explorer listening"
    );
    axum::serve(listener, api::router(explorer))
        .with_graceful_shutdown(shutdown())
        .await
        .map_err(|e| format!("serve: {e}"))?;
    tracing::info!("explorer stopped cleanly");
    Ok(())
}

/// Resolves on SIGTERM or Ctrl-C.
async fn shutdown() {
    let ctrl_c = async {
        let _ = tokio::signal::ctrl_c().await;
    };
    #[cfg(unix)]
    let term = async {
        match tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate()) {
            Ok(mut s) => {
                s.recv().await;
            }
            Err(_) => std::future::pending::<()>().await,
        }
    };
    #[cfg(not(unix))]
    let term = std::future::pending::<()>();
    tokio::select! {
        _ = ctrl_c => {},
        _ = term => {},
    }
    tracing::info!("shutdown signal received");
}
