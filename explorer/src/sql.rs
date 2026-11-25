//! The SQL of spec §13.4. Request values arrive only as validated types and
//! are embedded as single-quoted literals, which is safe because the
//! validation patterns exclude quotes (spec §13.5).

use crate::validate::{AssetId, ListParams};

const ASSET_COLUMNS: &str = "id, hostname, kind, os, site, owner_user_id, owner_name, group_id, group_name, version, changed_at";

/// `GET /api/assets/{id}`.
pub fn asset(id: &AssetId) -> String {
    format!(
        "SELECT {ASSET_COLUMNS} FROM inventory.v_assets_enriched WHERE id = '{}'",
        id.as_str()
    )
}

/// `GET /api/assets`.
pub fn list(p: &ListParams) -> String {
    let mut conditions = Vec::new();
    if let Some(site) = &p.site {
        conditions.push(format!("site = '{}'", site.as_str()));
    }
    if let Some(os) = &p.os {
        conditions.push(format!("os = '{}'", os.as_str()));
    }
    let filter = if conditions.is_empty() {
        String::new()
    } else {
        format!(" WHERE {}", conditions.join(" AND "))
    };
    format!(
        "SELECT {ASSET_COLUMNS} FROM inventory.v_assets_enriched{filter} ORDER BY id LIMIT {} OFFSET {}",
        p.limit, p.offset
    )
}

/// `GET /api/assets/{id}/history`.
pub fn history(id: &AssetId) -> String {
    format!(
        "SELECT version, hostname, kind, os, site, owner_user_id, changed_at FROM lake.inventory.asset_changes WHERE id = '{}' ORDER BY version",
        id.as_str()
    )
}

/// `GET /api/stats/os-site`.
pub fn os_site() -> String {
    "SELECT os, site, assets FROM inventory.mv_assets_by_os_site ORDER BY os, site".into()
}

/// `GET /api/stats/change-rate`. The global time zone is UTC (spec §11.7).
pub fn change_rate(minutes: u32) -> String {
    format!(
        "SELECT window_start, window_end, entity_type, changes FROM inventory.change_rate_1m WHERE window_start >= DATE_SUB(NOW(), INTERVAL {minutes} MINUTE) ORDER BY window_start, entity_type"
    )
}

/// `/readyz`.
pub const PING: &str = "SELECT 1";

#[cfg(test)]
mod tests {
    use super::*;
    use crate::validate::Filter;

    #[test]
    fn statements_match_the_spec() {
        let id = AssetId::parse("LT-0042").unwrap();
        assert_eq!(
            asset(&id),
            "SELECT id, hostname, kind, os, site, owner_user_id, owner_name, group_id, group_name, version, changed_at FROM inventory.v_assets_enriched WHERE id = 'LT-0042'"
        );
        assert_eq!(
            history(&id),
            "SELECT version, hostname, kind, os, site, owner_user_id, changed_at FROM lake.inventory.asset_changes WHERE id = 'LT-0042' ORDER BY version"
        );
        assert_eq!(
            os_site(),
            "SELECT os, site, assets FROM inventory.mv_assets_by_os_site ORDER BY os, site"
        );
        assert_eq!(
            change_rate(60),
            "SELECT window_start, window_end, entity_type, changes FROM inventory.change_rate_1m WHERE window_start >= DATE_SUB(NOW(), INTERVAL 60 MINUTE) ORDER BY window_start, entity_type"
        );
    }

    #[test]
    fn list_filters() {
        let base = "SELECT id, hostname, kind, os, site, owner_user_id, owner_name, group_id, group_name, version, changed_at FROM inventory.v_assets_enriched";
        let mut p = ListParams {
            site: None,
            os: None,
            limit: 50,
            offset: 0,
        };
        assert_eq!(list(&p), format!("{base} ORDER BY id LIMIT 50 OFFSET 0"));
        p.site = Some(Filter::parse("site", "Madrid").unwrap());
        p.limit = 10;
        p.offset = 10;
        assert_eq!(
            list(&p),
            format!("{base} WHERE site = 'Madrid' ORDER BY id LIMIT 10 OFFSET 10")
        );
        p.os = Some(Filter::parse("os", "Windows 11").unwrap());
        assert_eq!(
            list(&p),
            format!(
                "{base} WHERE site = 'Madrid' AND os = 'Windows 11' ORDER BY id LIMIT 10 OFFSET 10"
            )
        );
        p.site = None;
        assert_eq!(
            list(&p),
            format!("{base} WHERE os = 'Windows 11' ORDER BY id LIMIT 10 OFFSET 10")
        );
    }
}
