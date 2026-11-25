//! Input validation (spec §13.5). A request value reaches SQL only through the
//! types of this module, whose constructors are the only way to build them.

use std::collections::HashMap;

use crate::error::ApiError;

/// A validated asset id: `^(LT|DT|SV)-\d{4}$`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AssetId(String);

impl AssetId {
    pub fn parse(s: &str) -> Result<Self, ApiError> {
        let b = s.as_bytes();
        let ok = b.len() == 7
            && matches!(&b[..3], b"LT-" | b"DT-" | b"SV-")
            && b[3..].iter().all(u8::is_ascii_digit);
        if ok {
            Ok(AssetId(s.to_string()))
        } else {
            Err(ApiError::InvalidArgument(format!(
                "invalid asset id {:?}: want (LT|DT|SV)-NNNN",
                truncate(s)
            )))
        }
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// A validated `site` or `os` filter: `^[A-Za-z0-9 .\-]{1,64}$`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Filter(String);

impl Filter {
    pub fn parse(name: &str, s: &str) -> Result<Self, ApiError> {
        let ok = (1..=64).contains(&s.len())
            && s.bytes()
                .all(|c| c.is_ascii_alphanumeric() || matches!(c, b' ' | b'.' | b'-'));
        if ok {
            Ok(Filter(s.to_string()))
        } else {
            Err(ApiError::InvalidArgument(format!(
                "invalid {name} {:?}: 1 to 64 letters, digits, spaces, dots or dashes",
                truncate(s)
            )))
        }
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// Validated parameters of `GET /api/assets`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ListParams {
    pub site: Option<Filter>,
    pub os: Option<Filter>,
    pub limit: u32,
    pub offset: u64,
}

pub const LIMIT_DEFAULT: u32 = 50;
pub const LIMIT_MAX: u32 = 500;
pub const MINUTES_DEFAULT: u32 = 60;
pub const MINUTES_MAX: u32 = 1440;

impl ListParams {
    pub fn parse(q: &HashMap<String, String>) -> Result<Self, ApiError> {
        let site = q
            .get("site")
            .map(|s| Filter::parse("site", s))
            .transpose()?;
        let os = q.get("os").map(|s| Filter::parse("os", s)).transpose()?;
        let limit = match q.get("limit") {
            None => LIMIT_DEFAULT,
            Some(s) => int_in_range("limit", s, 1, u64::from(LIMIT_MAX))? as u32,
        };
        let offset = match q.get("offset") {
            None => 0,
            Some(s) => int_in_range("offset", s, 0, i64::MAX as u64)?,
        };
        Ok(ListParams {
            site,
            os,
            limit,
            offset,
        })
    }
}

/// Validated `minutes` of `GET /api/stats/change-rate` (1 to 1440).
pub fn minutes(q: &HashMap<String, String>) -> Result<u32, ApiError> {
    match q.get("minutes") {
        None => Ok(MINUTES_DEFAULT),
        Some(s) => Ok(int_in_range("minutes", s, 1, u64::from(MINUTES_MAX))? as u32),
    }
}

/// Parses a plain decimal integer (ASCII digits only, no sign) within [lo, hi].
fn int_in_range(name: &str, s: &str, lo: u64, hi: u64) -> Result<u64, ApiError> {
    let err = || {
        ApiError::InvalidArgument(format!(
            "invalid {name} {:?}: want an integer from {lo} to {hi}",
            truncate(s)
        ))
    };
    if s.is_empty() || s.len() > 20 || !s.bytes().all(|c| c.is_ascii_digit()) {
        return Err(err());
    }
    let v: u64 = s.parse().map_err(|_| err())?;
    if v < lo || v > hi {
        return Err(err());
    }
    Ok(v)
}

/// Keeps error messages short whatever the client sends.
fn truncate(s: &str) -> String {
    s.chars().take(80).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn q(pairs: &[(&str, &str)]) -> HashMap<String, String> {
        pairs
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect()
    }

    const INJECTIONS: &[&str] = &[
        "LT-0042' OR '1'='1",
        "LT-0042'",
        "'; DROP TABLE inventory.assets; --",
        "LT-0042 --",
        "LT-0042\u{2019}",
        "LT-0042%27",
        "LT-0042\\",
        "LT-0042\"",
        "LT-0042;",
        "LT-0042\n",
    ];

    #[test]
    fn asset_ids() {
        for ok in ["LT-0042", "DT-0001", "SV-0300", "LT-9999", "LT-0000"] {
            assert_eq!(AssetId::parse(ok).unwrap().as_str(), ok);
        }
        for bad in [
            "",
            "LT-42",
            "LT-00420",
            "lt-0042",
            "XX-0042",
            "LT0042",
            "LT-004a",
            " LT-0042",
            "LT-0042 ",
            "U-0117",
            "LT-٠٠٤٢",
        ] {
            assert!(AssetId::parse(bad).is_err(), "{bad:?} should be rejected");
        }
        for bad in INJECTIONS {
            assert!(AssetId::parse(bad).is_err(), "{bad:?} should be rejected");
        }
    }

    #[test]
    fn filters() {
        for ok in [
            "Madrid",
            "Windows 11",
            "macOS 15",
            "Ubuntu 24.04",
            "Windows Server 2022",
            "a-b",
        ] {
            assert_eq!(Filter::parse("site", ok).unwrap().as_str(), ok);
        }
        let long = "a".repeat(65);
        for bad in [
            "",
            long.as_str(),
            "Madrid'",
            "Mad_rid",
            "Málaga",
            "a%b",
            "x;y",
            "a/b",
        ] {
            assert!(
                Filter::parse("site", bad).is_err(),
                "{bad:?} should be rejected"
            );
        }
        // Every injection that needs a quote, a semicolon or a backslash is
        // rejected. A bare "--" is allowed by the pattern and harmless: the
        // value is always inside a single-quoted literal.
        for bad in INJECTIONS.iter().filter(|s| !s.ends_with(" --")) {
            assert!(
                Filter::parse("os", bad).is_err(),
                "{bad:?} should be rejected"
            );
        }
        assert!(Filter::parse("os", "LT-0042 --").is_ok());
        assert!(Filter::parse("site", &"a".repeat(64)).is_ok());
    }

    #[test]
    fn list_params() {
        let p = ListParams::parse(&q(&[])).unwrap();
        assert_eq!((p.limit, p.offset, p.site, p.os), (50, 0, None, None));
        let p = ListParams::parse(&q(&[("site", "Madrid"), ("limit", "10"), ("offset", "10")]))
            .unwrap();
        assert_eq!(p.site.unwrap().as_str(), "Madrid");
        assert_eq!((p.limit, p.offset), (10, 10));
        assert_eq!(
            ListParams::parse(&q(&[("limit", "500")])).unwrap().limit,
            500
        );
        for bad in [
            ("limit", "0"),
            ("limit", "501"),
            ("limit", "-1"),
            ("limit", "+5"),
            ("limit", "abc"),
            ("limit", "1e3"),
            ("limit", ""),
            ("limit", "10 "),
            ("offset", "-1"),
            ("offset", "99999999999999999999999"),
            ("offset", "9223372036854775808"),
            ("site", ""),
            ("site", "Madrid' OR '1'='1"),
            ("os", "x'--"),
        ] {
            assert!(
                ListParams::parse(&q(&[bad])).is_err(),
                "{bad:?} should be rejected"
            );
        }
        assert_eq!(
            ListParams::parse(&q(&[("offset", "9223372036854775807")]))
                .unwrap()
                .offset,
            i64::MAX as u64
        );
    }

    #[test]
    fn minutes_param() {
        assert_eq!(minutes(&q(&[])).unwrap(), 60);
        assert_eq!(minutes(&q(&[("minutes", "1")])).unwrap(), 1);
        assert_eq!(minutes(&q(&[("minutes", "1440")])).unwrap(), 1440);
        for bad in ["0", "1441", "-5", "sixty", "", "60.5", "60'"] {
            assert!(
                minutes(&q(&[("minutes", bad)])).is_err(),
                "{bad:?} should be rejected"
            );
        }
    }
}
