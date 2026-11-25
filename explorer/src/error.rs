//! Errors and their HTTP mapping (spec §13.2, §13.3, §13.6).

use axum::Json;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use serde_json::json;

/// An error answered to the client with the §13.3 error body.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ApiError {
    /// 400 `invalid_argument`.
    InvalidArgument(String),
    /// 404 `not_found`.
    NotFound(String),
    /// 503 `upstream_unavailable`: Doris unreachable, timed out or failing.
    Upstream(String),
    /// 503 `lake_unavailable`: the lake is disabled or Doris cannot read it.
    LakeUnavailable(String),
}

impl ApiError {
    pub fn status(&self) -> StatusCode {
        match self {
            ApiError::InvalidArgument(_) => StatusCode::BAD_REQUEST,
            ApiError::NotFound(_) => StatusCode::NOT_FOUND,
            ApiError::Upstream(_) | ApiError::LakeUnavailable(_) => StatusCode::SERVICE_UNAVAILABLE,
        }
    }

    pub fn code(&self) -> &'static str {
        match self {
            ApiError::InvalidArgument(_) => "invalid_argument",
            ApiError::NotFound(_) => "not_found",
            ApiError::Upstream(_) => "upstream_unavailable",
            ApiError::LakeUnavailable(_) => "lake_unavailable",
        }
    }

    pub fn message(&self) -> &str {
        match self {
            ApiError::InvalidArgument(m)
            | ApiError::NotFound(m)
            | ApiError::Upstream(m)
            | ApiError::LakeUnavailable(m) => m,
        }
    }
}

impl IntoResponse for ApiError {
    fn into_response(self) -> Response {
        if self.status().is_server_error() {
            tracing::warn!(
                error = self.code(),
                message = self.message(),
                "request failed"
            );
        }
        let body = Json(json!({"error": self.code(), "message": self.message()}));
        (self.status(), body).into_response()
    }
}

/// A failure talking to Doris.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DorisError {
    /// The bearer token was rejected: re-handshake once and retry (§13.6).
    Unauthenticated(String),
    /// The connection or stream failed: drop the session and reconnect on the
    /// next request (§13.6).
    Transport(String),
    /// Doris answered with an error for the statement.
    Query(String),
    /// The query did not finish within `QUERY_TIMEOUT_MS`.
    Timeout,
    /// Doris answered with data the explorer cannot map.
    Decode(String),
}

impl DorisError {
    /// Classifies a gRPC status from the FE or the BE.
    pub fn from_status(s: &tonic::Status) -> Self {
        use tonic::Code;
        let msg = format!("{:?}: {}", s.code(), s.message());
        match s.code() {
            Code::Unauthenticated => DorisError::Unauthenticated(msg),
            Code::Unavailable
            | Code::Cancelled
            | Code::DeadlineExceeded
            | Code::Unknown
            | Code::ResourceExhausted
            | Code::Aborted => DorisError::Transport(msg),
            _ => DorisError::Query(msg),
        }
    }

    pub fn message(&self) -> String {
        match self {
            DorisError::Unauthenticated(m)
            | DorisError::Transport(m)
            | DorisError::Query(m)
            | DorisError::Decode(m) => m.clone(),
            DorisError::Timeout => "the Doris query timed out".into(),
        }
    }

    /// Whether the error reports the lake catalog, the lake table or the
    /// object storage behind it as missing or unreachable.
    pub fn is_lake_problem(&self) -> bool {
        let DorisError::Query(m) = self else {
            return false;
        };
        let m = m.to_ascii_lowercase();
        let mentions_lake = m.contains("catalog")
            || m.contains("lake")
            || m.contains("asset_changes")
            || m.contains("iceberg");
        let missing = m.contains("not exist")
            || m.contains("unknown")
            || m.contains("not found")
            || m.contains("unknownhost")
            || m.contains("connect");
        (mentions_lake && missing) || m.contains("s3") || m.contains("rustfs")
    }

    /// Maps the error for a current-state endpoint.
    pub fn into_api(self) -> ApiError {
        ApiError::Upstream(self.message())
    }

    /// Maps the error for the history endpoint, which reads the lake.
    pub fn into_api_for_lake(self) -> ApiError {
        if self.is_lake_problem() {
            ApiError::LakeUnavailable(self.message())
        } else {
            ApiError::Upstream(self.message())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tonic::{Code, Status};

    #[test]
    fn api_errors_map_to_status_and_code() {
        let cases = [
            (
                ApiError::InvalidArgument("x".into()),
                400,
                "invalid_argument",
            ),
            (ApiError::NotFound("x".into()), 404, "not_found"),
            (ApiError::Upstream("x".into()), 503, "upstream_unavailable"),
            (
                ApiError::LakeUnavailable("x".into()),
                503,
                "lake_unavailable",
            ),
        ];
        for (e, status, code) in cases {
            assert_eq!(e.status().as_u16(), status, "{e:?}");
            assert_eq!(e.code(), code, "{e:?}");
        }
    }

    #[test]
    fn grpc_status_classification() {
        let cases = [
            (Code::Unauthenticated, "unauthenticated"),
            (Code::Unavailable, "transport"),
            (Code::Cancelled, "transport"),
            (Code::DeadlineExceeded, "transport"),
            (Code::Unknown, "transport"),
            (Code::Internal, "query"),
            (Code::InvalidArgument, "query"),
            (Code::NotFound, "query"),
            (Code::PermissionDenied, "query"),
        ];
        for (code, want) in cases {
            let got = match DorisError::from_status(&Status::new(code, "m")) {
                DorisError::Unauthenticated(_) => "unauthenticated",
                DorisError::Transport(_) => "transport",
                DorisError::Query(_) => "query",
                other => panic!("unexpected {other:?}"),
            };
            assert_eq!(got, want, "{code:?}");
        }
    }

    #[test]
    fn doris_errors_map_to_api_errors() {
        for e in [
            DorisError::Transport("t".into()),
            DorisError::Timeout,
            DorisError::Query("errCode = 2, detailMessage = Unknown column".into()),
            DorisError::Decode("d".into()),
            DorisError::Unauthenticated("u".into()),
        ] {
            assert_eq!(e.clone().into_api().code(), "upstream_unavailable", "{e:?}");
        }
        let lake = [
            "errCode = 2, detailMessage = Unknown catalog 'lake'",
            "errCode = 2, detailMessage = Table [asset_changes] does not exist in database [inventory].",
            "errCode = 2, detailMessage = UnknownHostException: warehouse.rustfs",
            "Internal: failed to load iceberg table: Connect to iceberg-rest:8181 failed",
        ];
        for m in lake {
            let e = DorisError::Query(m.into());
            assert_eq!(e.into_api_for_lake().code(), "lake_unavailable", "{m}");
        }
        assert_eq!(
            DorisError::Transport("Unavailable: catalog lake".into())
                .into_api_for_lake()
                .code(),
            "upstream_unavailable"
        );
        assert_eq!(
            DorisError::Query("errCode = 2, detailMessage = Unknown column 'x'".into())
                .into_api_for_lake()
                .code(),
            "upstream_unavailable"
        );
    }
}
