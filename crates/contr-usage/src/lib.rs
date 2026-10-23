//! Plan usage for contr's status bar: the usage model, with Claude Code's status
//! line and Codex's rollout logs as sources. No GPUI here, so `contr statusline`
//! runs without a window.

pub mod claude;
pub mod codex;
pub mod model;
