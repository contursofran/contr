//! The usage model: the plans contr shows, the limit windows a plan reports,
//! and the one number the status bar shows for each plan.

use serde::{Deserialize, Serialize};
use std::ffi::OsString;
use std::path::PathBuf;

/// A plan whose usage contr shows.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum PlanId {
    Claude,
    Codex,
}

/// The plans in bar order (docs/DESIGN.md). v0.1 has no OpenCode Go segment.
pub const PLANS: [PlanId; 2] = [PlanId::Claude, PlanId::Codex];

impl PlanId {
    /// The name shown in the bar.
    pub fn label(self) -> &'static str {
        match self {
            Self::Claude => "claude",
            Self::Codex => "codex",
        }
    }

    /// The id used in files and element ids.
    pub fn id(self) -> &'static str {
        match self {
            Self::Claude => "claude",
            Self::Codex => "codex",
        }
    }
}

/// One limit window, as its plan reports it.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct UsageWindow {
    /// "five_hour", "seven_day", "rolling", "weekly", "monthly", "primary" or "secondary".
    pub kind: String,
    /// 0 to 100 as the plan reports it; above 100 when the window is over.
    pub used_percent: f64,
    /// Unix seconds when the window resets; `None` when the plan does not say.
    pub resets_at: Option<u64>,
}

/// The windows one plan reported, and when they were taken.
#[derive(Debug, Clone, PartialEq)]
pub struct PlanUsage {
    pub plan: PlanId,
    pub windows: Vec<UsageWindow>,
    /// Unix seconds when the windows were taken.
    pub read_at: u64,
}

impl PlanUsage {
    /// The percent left for the bar, or `None` when the plan reported no window.
    pub fn left_percent(&self, now: u64) -> Option<f64> {
        left_of_tightest_window(&self.windows, now)
    }
}

/// Which window counts as "left" (owner question D60; recommended: the tighter
/// of the windows). Change the rule here only.
///
/// The result is 100 minus the highest used percent. A window whose
/// `resets_at` is at or before `now` has already reset and counts as 0 used.
/// The result is clamped to 0 to 100.
fn left_of_tightest_window(windows: &[UsageWindow], now: u64) -> Option<f64> {
    if windows.is_empty() {
        return None;
    }
    let most_used = windows
        .iter()
        .map(|window| match window.resets_at {
            Some(resets_at) if resets_at <= now => 0.0,
            _ => window.used_percent,
        })
        .fold(0.0_f64, f64::max);
    Some((100.0 - most_used).clamp(0.0, 100.0))
}

/// The bar's number: the left percent rounded, or "–" when there is no data.
pub fn left_label(left: Option<f64>) -> String {
    match left {
        Some(value) => format!("{}%", value.round() as i64),
        None => "–".to_string(),
    }
}

/// Where contr keeps its own files. See [`state_dir_from`].
pub fn state_dir() -> Option<PathBuf> {
    state_dir_from(
        |key| std::env::var_os(key),
        std::env::home_dir(),
        cfg!(target_os = "macos"),
    )
}

/// `$CONTR_STATE_DIR`; else on macOS `~/Library/Application Support/contr`;
/// else `$XDG_STATE_HOME/contr`, or `~/.local/state/contr`. Empty variables
/// count as unset. The environment and home are passed in so tests need no
/// real variables.
pub fn state_dir_from(
    env: impl Fn(&str) -> Option<OsString>,
    home: Option<PathBuf>,
    macos: bool,
) -> Option<PathBuf> {
    let set = |key: &str| env(key).filter(|value| !value.is_empty());
    if let Some(dir) = set("CONTR_STATE_DIR") {
        return Some(PathBuf::from(dir));
    }
    if macos {
        return home.map(|home| home.join("Library/Application Support/contr"));
    }
    if let Some(state) = set("XDG_STATE_HOME") {
        return Some(PathBuf::from(state).join("contr"));
    }
    home.map(|home| home.join(".local/state/contr"))
}

#[cfg(test)]
mod tests {
    use super::*;

    const RESET_LATER: Option<u64> = Some(2_000);

    fn window(kind: &str, used_percent: f64, resets_at: Option<u64>) -> UsageWindow {
        UsageWindow {
            kind: kind.to_string(),
            used_percent,
            resets_at,
        }
    }

    fn usage(windows: Vec<UsageWindow>) -> PlanUsage {
        PlanUsage {
            plan: PlanId::Claude,
            windows,
            read_at: 1_000,
        }
    }

    fn env_of(pairs: &'static [(&'static str, &'static str)]) -> impl Fn(&str) -> Option<OsString> {
        move |key| {
            pairs
                .iter()
                .find(|(name, _)| *name == key)
                .map(|(_, value)| OsString::from(value))
        }
    }

    #[test]
    fn bar_order_and_ids() {
        assert_eq!(PLANS, [PlanId::Claude, PlanId::Codex]);
        assert_eq!(PlanId::Claude.label(), "claude");
        assert_eq!(PlanId::Claude.id(), "claude");
        assert_eq!(PlanId::Codex.label(), "codex");
        assert_eq!(PlanId::Codex.id(), "codex");
    }

    #[test]
    fn left_takes_the_tighter_window() {
        let plan = usage(vec![
            window("five_hour", 23.5, RESET_LATER),
            window("seven_day", 41.2, RESET_LATER),
        ]);
        assert_eq!(plan.left_percent(1_000), Some(100.0 - 41.2));
    }

    #[test]
    fn a_window_past_its_reset_counts_as_unused() {
        let plan = usage(vec![
            window("five_hour", 90.0, Some(999)),
            window("seven_day", 30.0, RESET_LATER),
        ]);
        assert_eq!(plan.left_percent(1_000), Some(70.0));
    }

    #[test]
    fn a_window_resetting_exactly_now_counts_as_unused() {
        let plan = usage(vec![window("five_hour", 90.0, Some(1_000))]);
        assert_eq!(plan.left_percent(1_000), Some(100.0));
    }

    #[test]
    fn a_window_without_a_reset_time_counts_as_used() {
        let plan = usage(vec![window("primary", 40.0, None)]);
        assert_eq!(plan.left_percent(1_000), Some(60.0));
    }

    #[test]
    fn left_is_clamped_to_zero_to_hundred() {
        let over = usage(vec![window("five_hour", 150.0, RESET_LATER)]);
        assert_eq!(over.left_percent(1_000), Some(0.0));
        let negative = usage(vec![window("five_hour", -5.0, RESET_LATER)]);
        assert_eq!(negative.left_percent(1_000), Some(100.0));
    }

    #[test]
    fn left_is_none_without_windows() {
        assert_eq!(usage(Vec::new()).left_percent(1_000), None);
    }

    #[test]
    fn label_rounds_and_dashes_without_data() {
        assert_eq!(left_label(Some(58.8)), "59%");
        assert_eq!(left_label(Some(0.4)), "0%");
        assert_eq!(left_label(Some(100.0)), "100%");
        assert_eq!(left_label(None), "–");
    }

    #[test]
    fn state_dir_prefers_the_override() {
        let env = env_of(&[
            ("CONTR_STATE_DIR", "/x/state"),
            ("XDG_STATE_HOME", "/x/xdg"),
        ]);
        let home = Some(PathBuf::from("/h"));
        assert_eq!(
            state_dir_from(env, home.clone(), true),
            Some(PathBuf::from("/x/state"))
        );
        assert_eq!(
            state_dir_from(env_of(&[("CONTR_STATE_DIR", "/x/state")]), home, false),
            Some(PathBuf::from("/x/state"))
        );
    }

    #[test]
    fn state_dir_is_app_support_on_macos() {
        let home = Some(PathBuf::from("/h"));
        assert_eq!(
            state_dir_from(env_of(&[]), home, true),
            Some(PathBuf::from("/h/Library/Application Support/contr"))
        );
    }

    #[test]
    fn state_dir_follows_xdg_elsewhere() {
        let home = Some(PathBuf::from("/h"));
        assert_eq!(
            state_dir_from(env_of(&[("XDG_STATE_HOME", "/x")]), home.clone(), false),
            Some(PathBuf::from("/x/contr"))
        );
        assert_eq!(
            state_dir_from(env_of(&[]), home, false),
            Some(PathBuf::from("/h/.local/state/contr"))
        );
    }

    #[test]
    fn empty_variables_count_as_unset() {
        let home = Some(PathBuf::from("/h"));
        let env = env_of(&[("CONTR_STATE_DIR", ""), ("XDG_STATE_HOME", "")]);
        assert_eq!(
            state_dir_from(env, home, false),
            Some(PathBuf::from("/h/.local/state/contr"))
        );
    }

    #[test]
    fn no_home_and_no_override_is_none() {
        assert_eq!(state_dir_from(env_of(&[]), None, true), None);
        assert_eq!(state_dir_from(env_of(&[]), None, false), None);
    }
}
