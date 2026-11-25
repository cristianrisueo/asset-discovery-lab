//! The explorer's use cases: validated input in, §13.3 JSON out.

use std::sync::Arc;

use arrow_array::RecordBatch;
use serde_json::{Value, json};

use crate::convert::{Row, opt, rows};
use crate::error::{ApiError, DorisError};
use crate::flight::DorisQuery;
use crate::sql;
use crate::validate::{AssetId, ListParams};

/// The service layer over a Doris transport.
pub struct Explorer {
    doris: Arc<dyn DorisQuery>,
    lake_enabled: bool,
}

/// The `/readyz` outcome.
pub struct Readiness {
    pub transport: &'static str,
    pub error: Option<String>,
}

impl Explorer {
    pub fn new(doris: Arc<dyn DorisQuery>, lake_enabled: bool) -> Self {
        Explorer {
            doris,
            lake_enabled,
        }
    }

    async fn query(&self, sql: &str) -> Result<Vec<RecordBatch>, DorisError> {
        self.doris.query(sql).await
    }

    /// `SELECT 1` through the transport (spec §13.2).
    pub async fn ready(&self) -> Readiness {
        let error = match self.query(sql::PING).await {
            Ok(_) => None,
            Err(e) => Some(e.message()),
        };
        Readiness {
            transport: self.doris.transport(),
            error,
        }
    }

    pub async fn asset(&self, id: &AssetId) -> Result<Value, ApiError> {
        let batches = self
            .query(&sql::asset(id))
            .await
            .map_err(DorisError::into_api)?;
        let assets = map_rows(&batches, asset_json).map_err(DorisError::into_api)?;
        assets
            .into_iter()
            .next()
            .ok_or_else(|| ApiError::NotFound(format!("asset {} not found", id.as_str())))
    }

    pub async fn list(&self, p: &ListParams) -> Result<Value, ApiError> {
        let batches = self
            .query(&sql::list(p))
            .await
            .map_err(DorisError::into_api)?;
        let items = map_rows(&batches, asset_json).map_err(DorisError::into_api)?;
        Ok(json!({"items": items, "limit": p.limit, "offset": p.offset}))
    }

    pub async fn history(&self, id: &AssetId) -> Result<Value, ApiError> {
        if !self.lake_enabled {
            return Err(ApiError::LakeUnavailable(
                "the lake is disabled (LAKE_ENABLED=false)".into(),
            ));
        }
        let batches = self
            .query(&sql::history(id))
            .await
            .map_err(DorisError::into_api_for_lake)?;
        let versions = map_rows(&batches, version_json).map_err(DorisError::into_api)?;
        if versions.is_empty() {
            return Err(ApiError::NotFound(format!(
                "asset {} has no versions in the lake",
                id.as_str()
            )));
        }
        Ok(json!({"id": id.as_str(), "versions": versions}))
    }

    pub async fn os_site(&self) -> Result<Value, ApiError> {
        let batches = self
            .query(&sql::os_site())
            .await
            .map_err(DorisError::into_api)?;
        let items = map_rows(&batches, |r| {
            Ok(json!({"os": r.str("os")?, "site": r.str("site")?, "assets": r.i64("assets")?}))
        })
        .map_err(DorisError::into_api)?;
        Ok(json!({"items": items}))
    }

    pub async fn change_rate(&self, minutes: u32) -> Result<Value, ApiError> {
        let batches = self
            .query(&sql::change_rate(minutes))
            .await
            .map_err(DorisError::into_api)?;
        let items = map_rows(&batches, |r| {
            Ok(json!({
                "window_start": r.datetime("window_start")?,
                "window_end": r.datetime("window_end")?,
                "entity_type": r.str("entity_type")?,
                "changes": r.i64("changes")?,
            }))
        })
        .map_err(DorisError::into_api)?;
        Ok(json!({"items": items}))
    }
}

fn map_rows(
    batches: &[RecordBatch],
    f: impl Fn(&Row<'_>) -> Result<Value, DorisError>,
) -> Result<Vec<Value>, DorisError> {
    rows(batches).map(|r| f(&r)).collect()
}

/// A row of `v_assets_enriched` as the §13.3 Asset.
pub fn asset_json(r: &Row<'_>) -> Result<Value, DorisError> {
    let owner = match r.opt_ref("owner_user_id")? {
        None => Value::Null,
        Some(user_id) => json!({
            "user_id": user_id,
            "name": opt(r.opt_str("owner_name")?),
            "group_id": opt(r.opt_ref("group_id")?),
            "group": opt(r.opt_str("group_name")?),
        }),
    };
    Ok(json!({
        "id": r.str("id")?,
        "hostname": r.str("hostname")?,
        "kind": r.str("kind")?,
        "os": r.str("os")?,
        "site": r.str("site")?,
        "owner": owner,
        "version": r.i64("version")?,
        "changed_at": r.datetime("changed_at")?,
    }))
}

/// A row of `lake.inventory.asset_changes` as one §13.3 History version.
pub fn version_json(r: &Row<'_>) -> Result<Value, DorisError> {
    Ok(json!({
        "version": r.i64("version")?,
        "hostname": r.str("hostname")?,
        "kind": r.str("kind")?,
        "os": r.str("os")?,
        "site": r.str("site")?,
        "owner_user_id": opt(r.opt_ref("owner_user_id")?),
        "changed_at": r.datetime("changed_at")?,
    }))
}
