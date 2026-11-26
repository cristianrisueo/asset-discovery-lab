//! Structured JSON logs through `tracing`, with `service=explorer` on every
//! line, including the events of tonic and hyper (spec §13.8, docs/development/CLAUDE.build.md).

use std::fmt;

use chrono::{SecondsFormat, Utc};
use serde_json::{Map, Value};
use tracing::field::{Field, Visit};
use tracing::{Event, Subscriber};
use tracing_subscriber::EnvFilter;
use tracing_subscriber::fmt::format::Writer;
use tracing_subscriber::fmt::{FmtContext, FormatEvent, FormatFields};
use tracing_subscriber::registry::LookupSpan;

pub const SERVICE: &str = "explorer";

/// Installs the global subscriber. `filter` follows `RUST_LOG` syntax.
pub fn init(filter: &str) {
    let filter = EnvFilter::try_new(filter).unwrap_or_else(|_| EnvFilter::new("info"));
    tracing_subscriber::fmt()
        .with_env_filter(filter)
        .event_format(Json)
        .with_writer(std::io::stdout)
        .init();
}

/// One JSON object per event.
pub struct Json;

impl<S, N> FormatEvent<S, N> for Json
where
    S: Subscriber + for<'a> LookupSpan<'a>,
    N: for<'a> FormatFields<'a> + 'static,
{
    fn format_event(
        &self,
        _ctx: &FmtContext<'_, S, N>,
        mut writer: Writer<'_>,
        event: &Event<'_>,
    ) -> fmt::Result {
        writeln!(writer, "{}", Value::Object(event_json(event)))
    }
}

fn event_json(event: &Event<'_>) -> Map<String, Value> {
    let meta = event.metadata();
    let mut m = Map::new();
    m.insert(
        "time".into(),
        Utc::now()
            .to_rfc3339_opts(SecondsFormat::Millis, true)
            .into(),
    );
    m.insert(
        "level".into(),
        meta.level().as_str().to_ascii_lowercase().into(),
    );
    m.insert("service".into(), SERVICE.into());
    m.insert("target".into(), meta.target().into());
    event.record(&mut Fields(&mut m));
    m.insert("service".into(), SERVICE.into());
    m
}

struct Fields<'a>(&'a mut Map<String, Value>);

impl Visit for Fields<'_> {
    fn record_str(&mut self, f: &Field, v: &str) {
        self.0.insert(name(f), v.into());
    }
    fn record_i64(&mut self, f: &Field, v: i64) {
        self.0.insert(name(f), v.into());
    }
    fn record_u64(&mut self, f: &Field, v: u64) {
        self.0.insert(name(f), v.into());
    }
    fn record_f64(&mut self, f: &Field, v: f64) {
        self.0.insert(name(f), v.into());
    }
    fn record_bool(&mut self, f: &Field, v: bool) {
        self.0.insert(name(f), v.into());
    }
    fn record_debug(&mut self, f: &Field, v: &dyn fmt::Debug) {
        self.0.insert(name(f), format!("{v:?}").into());
    }
}

fn name(f: &Field) -> String {
    match f.name() {
        "message" => "msg".into(),
        n => n.into(),
    }
}
