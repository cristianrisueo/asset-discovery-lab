//! HTTP transport (spec §13.2): routes, input extraction and responses.

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;

use axum::extract::rejection::{PathRejection, QueryRejection};
use axum::extract::{MatchedPath, Path, Query, Request, State};
use axum::http::StatusCode;
use axum::middleware::{self, Next};
use axum::response::{IntoResponse, Response};
use axum::routing::get;
use axum::{Json, Router};
use serde_json::{Value, json};

use crate::error::ApiError;
use crate::service::Explorer;
use crate::validate::{self, AssetId, ListParams};

type AppState = Arc<Explorer>;
type ApiResult = Result<Json<Value>, ApiError>;
type Params = Result<Query<HashMap<String, String>>, QueryRejection>;

/// Builds the router with the request log.
pub fn router(explorer: Arc<Explorer>) -> Router {
    Router::new()
        .route("/healthz", get(healthz))
        .route("/readyz", get(readyz))
        .route("/api/assets", get(list))
        .route("/api/assets/{id}", get(asset))
        .route("/api/assets/{id}/history", get(history))
        .route("/api/stats/os-site", get(os_site))
        .route("/api/stats/change-rate", get(change_rate))
        .fallback(not_found)
        .layer(middleware::from_fn(log_request))
        .with_state(explorer)
}

async fn healthz() -> Json<Value> {
    Json(json!({"status": "ok"}))
}

async fn readyz(State(x): State<AppState>) -> Response {
    let r = x.ready().await;
    match r.error {
        None => Json(json!({"status": "ready", "doris_transport": r.transport})).into_response(),
        Some(error) => (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(json!({"status": "not_ready", "doris_transport": r.transport, "error": error})),
        )
            .into_response(),
    }
}

async fn asset(State(x): State<AppState>, id: Result<Path<String>, PathRejection>) -> ApiResult {
    let id = asset_id(id)?;
    x.asset(&id).await.map(Json)
}

async fn history(State(x): State<AppState>, id: Result<Path<String>, PathRejection>) -> ApiResult {
    let id = asset_id(id)?;
    x.history(&id).await.map(Json)
}

async fn list(State(x): State<AppState>, q: Params) -> ApiResult {
    let p = ListParams::parse(&params(q)?)?;
    x.list(&p).await.map(Json)
}

async fn os_site(State(x): State<AppState>) -> ApiResult {
    x.os_site().await.map(Json)
}

async fn change_rate(State(x): State<AppState>, q: Params) -> ApiResult {
    let minutes = validate::minutes(&params(q)?)?;
    x.change_rate(minutes).await.map(Json)
}

async fn not_found(req: Request) -> ApiError {
    ApiError::NotFound(format!("no route for {}", req.uri().path()))
}

/// The path id, validated. Axum's own rejections (for example a path that
/// is not valid UTF-8 once decoded) also become 400 `invalid_argument`.
fn asset_id(p: Result<Path<String>, PathRejection>) -> Result<AssetId, ApiError> {
    let Path(raw) = p.map_err(|e| ApiError::InvalidArgument(format!("invalid path: {e}")))?;
    AssetId::parse(&raw)
}

fn params(q: Params) -> Result<HashMap<String, String>, ApiError> {
    q.map(|Query(m)| m)
        .map_err(|e| ApiError::InvalidArgument(format!("invalid query string: {e}")))
}

/// One JSON log line per request: route, method, status and latency (§13.8).
async fn log_request(req: Request, next: Next) -> Response {
    let route = req
        .extensions()
        .get::<MatchedPath>()
        .map(|m| m.as_str().to_string())
        .unwrap_or_else(|| "unmatched".into());
    let method = req.method().clone();
    let start = Instant::now();
    let resp = next.run(req).await;
    let latency_ms = start.elapsed().as_secs_f64() * 1000.0;
    let status = resp.status().as_u16();
    let latency_ms = (latency_ms * 1000.0).round() / 1000.0;
    if route == "/healthz" {
        // The compose health check calls it every 5 s; keep it out of info.
        tracing::debug!(route = %route, method = %method, status, latency_ms, "request");
        return resp;
    }
    tracing::info!(
        route = %route,
        method = %method,
        status,
        latency_ms,
        "request"
    );
    resp
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::error::DorisError;
    use crate::flight::DorisQuery;
    use arrow_array::{
        ArrayRef, Int64Array, RecordBatch, StringArray, TimestampMicrosecondArray,
        TimestampMillisecondArray,
    };
    use axum::body::Body;
    use axum::http::Request as HttpRequest;
    use futures::future::BoxFuture;
    use http_body_util::BodyExt;
    use std::sync::Mutex;
    use tower::ServiceExt;

    // 2026-11-25T10:14:03.120Z
    const MS: i64 = 1_795_601_643_120;

    /// A fake transport that answers every statement with the same result and
    /// remembers the statements it saw.
    struct Fake {
        result: Result<Vec<RecordBatch>, DorisError>,
        seen: Mutex<Vec<String>>,
    }

    impl DorisQuery for Fake {
        fn query<'a>(
            &'a self,
            sql: &'a str,
        ) -> BoxFuture<'a, Result<Vec<RecordBatch>, DorisError>> {
            self.seen.lock().unwrap().push(sql.to_string());
            let r = self.result.clone();
            Box::pin(async move { r })
        }
        fn transport(&self) -> &'static str {
            "flight-sql"
        }
    }

    fn app(result: Result<Vec<RecordBatch>, DorisError>, lake: bool) -> (Router, Arc<Fake>) {
        let fake = Arc::new(Fake {
            result,
            seen: Mutex::new(vec![]),
        });
        let x = Arc::new(Explorer::new(fake.clone(), lake));
        (router(x), fake)
    }

    async fn get(app: Router, uri: &str) -> (u16, Value) {
        let resp = app
            .oneshot(HttpRequest::get(uri).body(Body::empty()).unwrap())
            .await
            .unwrap();
        let status = resp.status().as_u16();
        let body = resp.into_body().collect().await.unwrap().to_bytes();
        let v = serde_json::from_slice(&body)
            .unwrap_or_else(|_| panic!("{uri}: body is not JSON: {body:?}"));
        (status, v)
    }

    fn s(v: Vec<Option<&str>>) -> ArrayRef {
        Arc::new(StringArray::from(v))
    }

    fn asset_batch(owner: Option<&str>, owner_name: Option<&str>) -> RecordBatch {
        RecordBatch::try_from_iter(vec![
            ("id", s(vec![Some("LT-0042")])),
            ("hostname", s(vec![Some("lt-0042.acme.local")])),
            ("kind", s(vec![Some("LAPTOP")])),
            ("os", s(vec![Some("Windows 11")])),
            ("site", s(vec![Some("Madrid")])),
            ("owner_user_id", s(vec![owner])),
            ("owner_name", s(vec![owner_name])),
            ("group_id", s(vec![owner_name.map(|_| "G-03")])),
            ("group_name", s(vec![owner_name.map(|_| "Ventas")])),
            ("version", Arc::new(Int64Array::from(vec![7])) as ArrayRef),
            (
                "changed_at",
                Arc::new(TimestampMicrosecondArray::from(vec![MS * 1000])) as ArrayRef,
            ),
        ])
        .unwrap()
    }

    #[tokio::test]
    async fn asset_matches_the_spec_shape() {
        let (app, fake) = app(
            Ok(vec![asset_batch(Some("U-0117"), Some("Ana Ruiz"))]),
            true,
        );
        let (status, v) = get(app, "/api/assets/LT-0042").await;
        assert_eq!(status, 200);
        assert_eq!(
            v,
            json!({
                "id": "LT-0042", "hostname": "lt-0042.acme.local", "kind": "LAPTOP",
                "os": "Windows 11", "site": "Madrid",
                "owner": {"user_id": "U-0117", "name": "Ana Ruiz", "group_id": "G-03", "group": "Ventas"},
                "version": 7, "changed_at": "2026-11-25T10:14:03.120Z"
            })
        );
        assert!(fake.seen.lock().unwrap()[0].ends_with("WHERE id = 'LT-0042'"));
    }

    #[tokio::test]
    async fn owner_null_and_owner_without_user_row() {
        let (a, _) = app(Ok(vec![asset_batch(None, None)]), true);
        let (_, v) = get(a, "/api/assets/LT-0042").await;
        assert_eq!(v["owner"], Value::Null);
        let (a, _) = app(Ok(vec![asset_batch(Some("U-0117"), None)]), true);
        let (_, v) = get(a, "/api/assets/LT-0042").await;
        assert_eq!(
            v["owner"],
            json!({"user_id": "U-0117", "name": null, "group_id": null, "group": null})
        );
    }

    #[tokio::test]
    async fn errors_and_validation() {
        // No row: 404.
        let (a, _) = app(Ok(vec![]), true);
        let (status, v) = get(a, "/api/assets/LT-9999").await;
        assert_eq!((status, v["error"].as_str()), (404, Some("not_found")));
        assert_eq!(v["message"], "asset LT-9999 not found");

        // The D02 URLs: no query reaches Doris.
        for uri in [
            "/api/assets/LT-0042'%20OR%20'1'='1",
            "/api/assets/LT-0042%27",
            "/api/assets/%FF%FE",
            "/api/assets?limit=0",
            "/api/assets?limit=abc",
            "/api/assets?site=Mad'rid",
            "/api/assets?offset=-1",
            "/api/stats/change-rate?minutes=0",
            "/api/stats/change-rate?minutes=1441",
            "/api/assets/lt-0042/history",
        ] {
            let (a, fake) = app(Ok(vec![]), true);
            let (status, v) = get(a, uri).await;
            assert_eq!(
                (status, v["error"].as_str()),
                (400, Some("invalid_argument")),
                "{uri}"
            );
            assert!(fake.seen.lock().unwrap().is_empty(), "{uri} reached Doris");
        }

        // Doris failures: 503 upstream_unavailable.
        for uri in ["/api/assets/LT-0042", "/api/assets", "/api/stats/os-site"] {
            let (a, _) = app(Err(DorisError::Transport("down".into())), true);
            let (status, v) = get(a, uri).await;
            assert_eq!(
                (status, v["error"].as_str()),
                (503, Some("upstream_unavailable")),
                "{uri}"
            );
        }
        let (a, _) = app(Err(DorisError::Timeout), true);
        assert_eq!(get(a, "/api/stats/change-rate").await.0, 503);

        // Unknown route: 404 JSON.
        let (a, _) = app(Ok(vec![]), true);
        let (status, v) = get(a, "/api/nope").await;
        assert_eq!((status, v["error"].as_str()), (404, Some("not_found")));
    }

    #[tokio::test]
    async fn history_and_the_lake() {
        let b = RecordBatch::try_from_iter(vec![
            (
                "version",
                Arc::new(Int64Array::from(vec![6, 7])) as ArrayRef,
            ),
            ("hostname", s(vec![Some("lt-0042.acme.local"); 2])),
            ("kind", s(vec![Some("LAPTOP"); 2])),
            ("os", s(vec![Some("Windows 11"); 2])),
            ("site", s(vec![Some("Alicante"), Some("Madrid")])),
            ("owner_user_id", s(vec![Some("U-0117"), None])),
            (
                "changed_at",
                Arc::new(
                    TimestampMicrosecondArray::from(vec![MS * 1000 - 1000, MS * 1000])
                        .with_timezone("UTC"),
                ) as ArrayRef,
            ),
        ])
        .unwrap();
        let (a, fake) = app(Ok(vec![b]), true);
        let (status, v) = get(a, "/api/assets/LT-0042/history").await;
        assert_eq!(status, 200);
        assert_eq!(v["id"], "LT-0042");
        assert_eq!(
            v["versions"][0],
            json!({"version": 6, "hostname": "lt-0042.acme.local", "kind": "LAPTOP", "os": "Windows 11",
                   "site": "Alicante", "owner_user_id": "U-0117", "changed_at": "2026-11-25T10:14:03.119Z"})
        );
        assert_eq!(v["versions"][1]["owner_user_id"], Value::Null);
        assert!(fake.seen.lock().unwrap()[0].contains("FROM lake.inventory.asset_changes"));

        // No versions: 404.
        let (a, _) = app(Ok(vec![]), true);
        assert_eq!(get(a, "/api/assets/LT-9999/history").await.0, 404);

        // Lake disabled: 503 lake_unavailable without a query.
        let (a, fake) = app(Ok(vec![]), false);
        let (status, v) = get(a, "/api/assets/LT-0042/history").await;
        assert_eq!(
            (status, v["error"].as_str()),
            (503, Some("lake_unavailable"))
        );
        assert!(fake.seen.lock().unwrap().is_empty());

        // The catalog is missing: 503 lake_unavailable.
        let (a, _) = app(
            Err(DorisError::Query(
                "errCode = 2, detailMessage = Unknown catalog 'lake'".into(),
            )),
            true,
        );
        let (status, v) = get(a, "/api/assets/LT-0042/history").await;
        assert_eq!(
            (status, v["error"].as_str()),
            (503, Some("lake_unavailable"))
        );
    }

    #[tokio::test]
    async fn list_and_stats() {
        let (a, fake) = app(Ok(vec![asset_batch(None, None)]), true);
        let (status, v) = get(a, "/api/assets?site=Madrid&limit=10&offset=10").await;
        assert_eq!(status, 200);
        assert_eq!(
            (v["limit"].as_i64(), v["offset"].as_i64()),
            (Some(10), Some(10))
        );
        assert_eq!(v["items"][0]["id"], "LT-0042");
        assert!(
            fake.seen.lock().unwrap()[0]
                .ends_with("WHERE site = 'Madrid' ORDER BY id LIMIT 10 OFFSET 10")
        );

        let b = RecordBatch::try_from_iter(vec![
            ("os", s(vec![Some("Ubuntu 24.04"), Some("Windows 11")])),
            ("site", s(vec![Some("Bilbao"), Some("Madrid")])),
            (
                "assets",
                Arc::new(Int64Array::from(vec![3, 250])) as ArrayRef,
            ),
        ])
        .unwrap();
        let (a, _) = app(Ok(vec![b]), true);
        let (_, v) = get(a, "/api/stats/os-site").await;
        assert_eq!(
            v,
            json!({"items": [
                {"os": "Ubuntu 24.04", "site": "Bilbao", "assets": 3},
                {"os": "Windows 11", "site": "Madrid", "assets": 250}
            ]})
        );

        let start = (MS / 60_000) * 60_000;
        let b = RecordBatch::try_from_iter(vec![
            (
                "window_start",
                Arc::new(TimestampMillisecondArray::from(vec![start])) as ArrayRef,
            ),
            (
                "window_end",
                Arc::new(TimestampMillisecondArray::from(vec![start + 60_000])) as ArrayRef,
            ),
            ("entity_type", s(vec![Some("asset")])),
            ("changes", Arc::new(Int64Array::from(vec![312])) as ArrayRef),
        ])
        .unwrap();
        let (a, fake) = app(Ok(vec![b]), true);
        let (_, v) = get(a, "/api/stats/change-rate?minutes=60").await;
        assert_eq!(
            v,
            json!({"items": [{"window_start": "2026-11-25T10:14:00.000Z",
                              "window_end": "2026-11-25T10:15:00.000Z",
                              "entity_type": "asset", "changes": 312}]})
        );
        assert!(fake.seen.lock().unwrap()[0].contains("INTERVAL 60 MINUTE"));
    }

    #[tokio::test]
    async fn health_and_readiness() {
        let (a, _) = app(Ok(vec![]), true);
        assert_eq!(get(a, "/healthz").await, (200, json!({"status": "ok"})));
        let (a, _) = app(Ok(vec![]), true);
        assert_eq!(
            get(a, "/readyz").await,
            (
                200,
                json!({"status": "ready", "doris_transport": "flight-sql"})
            )
        );
        let (a, _) = app(Err(DorisError::Transport("connect refused".into())), true);
        let (status, v) = get(a, "/readyz").await;
        assert_eq!(status, 503);
        assert_eq!(
            (v["status"].as_str(), v["doris_transport"].as_str()),
            (Some("not_ready"), Some("flight-sql"))
        );
        assert_eq!(v["error"], "connect refused");
    }
}
