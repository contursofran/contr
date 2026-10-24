//! The dev control wire format. Each side sends one JSON line: the client sends a request
//! and the dev window sends one reply, then the connection closes. This module has no GPUI
//! code, so the client and the tests use it in every build.

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// The environment variable that names the socket. The dev window opens it only when the
/// variable is set and not empty.
pub const ENV: &str = "CONTR_DEV_CONTROL";

/// The protocol version. Every request carries it in `v`.
pub const VERSION: u32 = 1;

/// The longest request line in bytes, without its newline.
pub const MAX_LINE: usize = 64 * 1024;

/// The error codes that a reply can carry.
pub mod code {
    pub const BAD_REQUEST: &str = "bad_request";
    pub const UNSUPPORTED_VERSION: &str = "unsupported_version";
    pub const UNKNOWN_ID: &str = "unknown_id";
    pub const DISABLED: &str = "disabled";
    pub const NOT_A_ROW: &str = "not_a_row";
    pub const BAD_KEY: &str = "bad_key";
    pub const BAD_SIZE: &str = "bad_size";
    pub const NO_WINDOW: &str = "no_window";
    pub const UNSUPPORTED: &str = "unsupported";
}

/// One request. `cmd` names the form, and the other fields hold its arguments.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "cmd", rename_all = "kebab-case")]
pub enum Request {
    State,
    Click { id: String },
    Key { keys: Vec<String> },
    Select { row: String },
    Resize { width: f32, height: f32 },
    WindowId,
}

/// The request with its version. `v` comes first on the wire.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
struct Envelope {
    v: u32,
    #[serde(flatten)]
    request: Request,
}

/// An error reply: a code from [`code`] and a message for people.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DevError {
    pub code: String,
    pub message: String,
}

impl DevError {
    pub fn new(code: &str, message: impl Into<String>) -> Self {
        Self {
            code: code.to_owned(),
            message: message.into(),
        }
    }
}

/// The reply to one request. `Ok` holds the result, and `Err` holds the error.
#[derive(Debug, Clone, PartialEq)]
pub enum Reply {
    Ok(Value),
    Err(DevError),
}

/// Reads one request line. The line may end with `\n`, which does not count toward
/// `MAX_LINE`. The version is checked before the request fields.
pub fn parse_request(line: &str) -> Result<Request, DevError> {
    let body = line.strip_suffix('\n').unwrap_or(line);
    if body.len() > MAX_LINE {
        return Err(DevError::new(
            code::BAD_REQUEST,
            format!("the request is longer than {MAX_LINE} bytes"),
        ));
    }
    let value: Value = serde_json::from_str(body).map_err(|error| {
        DevError::new(
            code::BAD_REQUEST,
            format!("the request is not JSON: {error}"),
        )
    })?;
    if !value.is_object() {
        return Err(DevError::new(
            code::BAD_REQUEST,
            "the request is not a JSON object",
        ));
    }
    if value.get("v").and_then(Value::as_u64) != Some(u64::from(VERSION)) {
        return Err(DevError::new(
            code::UNSUPPORTED_VERSION,
            format!("this window speaks protocol version {VERSION}"),
        ));
    }
    let envelope: Envelope = serde_json::from_value(value)
        .map_err(|error| DevError::new(code::BAD_REQUEST, format!("invalid request: {error}")))?;
    Ok(envelope.request)
}

/// The request line for `request`, with its newline.
pub fn request_line(request: &Request) -> String {
    let envelope = Envelope {
        v: VERSION,
        request: request.clone(),
    };
    // Every field is a string, a list of strings or a number, and serde_json writes a
    // non-finite number as null. So serialization cannot fail, and the empty fallback is never used.
    let body = serde_json::to_string(&envelope).unwrap_or_default();
    body + "\n"
}

/// The reply line for `reply`, with its newline. The keys are in the order of the contract:
/// `ok`, then `result` or `error`. Value's `Display` cannot fail, so this returns no `Result`.
pub fn reply_line(reply: &Reply) -> String {
    let body = match reply {
        Reply::Ok(result) => format!(r#""ok":true,"result":{result}"#),
        Reply::Err(error) => format!(
            r#""ok":false,"error":{{"code":{},"message":{}}}"#,
            Value::from(error.code.as_str()),
            Value::from(error.message.as_str()),
        ),
    };
    format!("{{{body}}}\n")
}

/// Reads one reply line, with or without its newline. A line that does not fit the
/// contract gives a message for the client's error line.
pub fn parse_reply(line: &str) -> Result<Reply, String> {
    let value: Value =
        serde_json::from_str(line).map_err(|error| format!("the reply is not JSON: {error}"))?;
    let Value::Object(mut fields) = value else {
        return Err("the reply is not a JSON object".to_owned());
    };
    match fields.remove("ok") {
        Some(Value::Bool(true)) => fields
            .remove("result")
            .map(Reply::Ok)
            .ok_or_else(|| "the reply has no result".to_owned()),
        Some(Value::Bool(false)) => match fields.remove("error") {
            Some(Value::Object(error)) => match (
                error.get("code").and_then(Value::as_str),
                error.get("message").and_then(Value::as_str),
            ) {
                (Some(code), Some(message)) => Ok(Reply::Err(DevError::new(code, message))),
                _ => Err("the error reply has no code or message".to_owned()),
            },
            _ => Err("the error reply has no error object".to_owned()),
        },
        _ => Err("the reply has no ok field".to_owned()),
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use serde_json::json;

    /// One of every request form, with ids that hold `:`, `/` and `#`.
    fn every_request() -> Vec<Request> {
        vec![
            Request::State,
            Request::Click {
                id: "wt:/abs/root#1".into(),
            },
            Request::Key {
                keys: vec!["down".into(), "enter".into()],
            },
            Request::Select {
                row: "agent:w1:p1".into(),
            },
            Request::Resize {
                width: 1200.0,
                height: 800.5,
            },
            Request::WindowId,
        ]
    }

    /// A request line with `body` in place of the object.
    fn line_of(body: &str) -> String {
        format!("{body}\n")
    }

    #[test]
    fn every_request_round_trips() {
        for request in every_request() {
            let line = request_line(&request);
            assert!(line.ends_with('\n'));
            assert_eq!(parse_request(&line).unwrap(), request);
        }
    }

    #[test]
    fn request_lines_match_the_contract() {
        assert_eq!(
            request_line(&Request::State),
            "{\"v\":1,\"cmd\":\"state\"}\n"
        );
        assert_eq!(
            request_line(&Request::Click {
                id: "terminal".into()
            }),
            "{\"v\":1,\"cmd\":\"click\",\"id\":\"terminal\"}\n"
        );
        assert_eq!(
            request_line(&Request::Key {
                keys: vec!["down".into(), "enter".into()]
            }),
            "{\"v\":1,\"cmd\":\"key\",\"keys\":[\"down\",\"enter\"]}\n"
        );
        assert_eq!(
            request_line(&Request::Select {
                row: "wt:/a".into()
            }),
            "{\"v\":1,\"cmd\":\"select\",\"row\":\"wt:/a\"}\n"
        );
        assert_eq!(
            request_line(&Request::Resize {
                width: 1200.0,
                height: 800.0
            }),
            "{\"v\":1,\"cmd\":\"resize\",\"width\":1200.0,\"height\":800.0}\n"
        );
        assert_eq!(
            request_line(&Request::WindowId),
            "{\"v\":1,\"cmd\":\"window-id\"}\n"
        );
    }

    #[test]
    fn a_request_without_its_newline_is_accepted() {
        assert_eq!(
            parse_request("{\"v\":1,\"cmd\":\"state\"}").unwrap(),
            Request::State
        );
    }

    #[test]
    fn a_missing_version_is_unsupported() {
        let error = parse_request(&line_of(r#"{"cmd":"state"}"#)).unwrap_err();
        assert_eq!(error.code, code::UNSUPPORTED_VERSION);
    }

    #[test]
    fn another_version_is_unsupported() {
        let error = parse_request(&line_of(r#"{"v":2,"cmd":"state"}"#)).unwrap_err();
        assert_eq!(error.code, code::UNSUPPORTED_VERSION);
        // A version that is not a whole number is not version 1 either.
        let error = parse_request(&line_of(r#"{"v":"1","cmd":"state"}"#)).unwrap_err();
        assert_eq!(error.code, code::UNSUPPORTED_VERSION);
    }

    #[test]
    fn the_version_is_checked_before_the_command() {
        let error = parse_request(&line_of(r#"{"v":2,"cmd":"bogus"}"#)).unwrap_err();
        assert_eq!(error.code, code::UNSUPPORTED_VERSION);
    }

    #[test]
    fn an_unknown_command_is_a_bad_request() {
        let error = parse_request(&line_of(r#"{"v":1,"cmd":"bogus"}"#)).unwrap_err();
        assert_eq!(error.code, code::BAD_REQUEST);
        assert!(
            error.message.starts_with("invalid request:"),
            "{}",
            error.message
        );
    }

    #[test]
    fn a_missing_field_is_a_bad_request() {
        let error = parse_request(&line_of(r#"{"v":1,"cmd":"click"}"#)).unwrap_err();
        assert_eq!(error.code, code::BAD_REQUEST);
        let error = parse_request(&line_of(r#"{"v":1,"cmd":"resize","width":1}"#)).unwrap_err();
        assert_eq!(error.code, code::BAD_REQUEST);
    }

    #[test]
    fn bad_json_is_a_bad_request() {
        for line in ["", "not json", "{\"v\":1,", "[1]\n", "\"state\""] {
            let error = parse_request(line).unwrap_err();
            assert_eq!(error.code, code::BAD_REQUEST, "line {line:?}");
        }
    }

    #[test]
    fn a_line_longer_than_the_limit_is_a_bad_request() {
        let padding = "a".repeat(MAX_LINE);
        let line = line_of(&format!(
            "{{\"v\":1,\"cmd\":\"click\",\"id\":\"{padding}\"}}"
        ));
        let error = parse_request(&line).unwrap_err();
        assert_eq!(error.code, code::BAD_REQUEST);
        assert!(error.message.contains("65536"), "{}", error.message);
    }

    #[test]
    fn a_line_at_the_limit_is_accepted() {
        // The newline does not count, so a body of exactly MAX_LINE bytes is allowed.
        let head = "{\"v\":1,\"cmd\":\"click\",\"id\":\"";
        let tail = "\"}";
        let padding = "a".repeat(MAX_LINE - head.len() - tail.len());
        let body = format!("{head}{padding}{tail}");
        assert_eq!(body.len(), MAX_LINE);
        let request = parse_request(&line_of(&body)).unwrap();
        assert_eq!(
            request,
            Request::Click {
                id: padding.clone()
            }
        );
    }

    #[test]
    fn an_ok_reply_serializes_with_its_result() {
        let line = reply_line(&Reply::Ok(json!({"window_id": 12345})));
        assert_eq!(line, "{\"ok\":true,\"result\":{\"window_id\":12345}}\n");
    }

    #[test]
    fn an_error_reply_serializes_as_in_the_contract() {
        let line = reply_line(&Reply::Err(DevError::new(code::UNKNOWN_ID, "no such id")));
        assert_eq!(
            line,
            "{\"ok\":false,\"error\":{\"code\":\"unknown_id\",\"message\":\"no such id\"}}\n"
        );
    }

    #[test]
    fn replies_round_trip() {
        let replies = [
            Reply::Ok(json!({"v": 1, "focus": "panel", "controls": []})),
            Reply::Err(DevError::new(code::BAD_KEY, "unknown key \"x\"")),
        ];
        for reply in replies {
            let line = reply_line(&reply);
            assert_eq!(parse_reply(&line).unwrap(), reply);
            // The newline is optional when a client reads a reply.
            assert_eq!(parse_reply(line.trim_end()).unwrap(), reply);
        }
    }

    #[test]
    fn a_reply_that_is_not_json_is_refused() {
        let error = parse_reply("nope\n").unwrap_err();
        assert!(error.starts_with("the reply is not JSON:"), "{error}");
    }

    #[test]
    fn a_reply_that_is_not_an_object_is_refused() {
        assert_eq!(
            parse_reply("[]\n").unwrap_err(),
            "the reply is not a JSON object"
        );
    }

    #[test]
    fn an_ok_reply_without_a_result_is_refused() {
        assert_eq!(
            parse_reply("{\"ok\":true}\n").unwrap_err(),
            "the reply has no result"
        );
    }

    #[test]
    fn an_error_reply_without_its_fields_is_refused() {
        assert_eq!(
            parse_reply("{\"ok\":false}\n").unwrap_err(),
            "the error reply has no error object"
        );
        assert_eq!(
            parse_reply("{\"ok\":false,\"error\":{\"code\":\"x\"}}\n").unwrap_err(),
            "the error reply has no code or message"
        );
    }

    #[test]
    fn a_reply_without_ok_is_refused() {
        assert_eq!(
            parse_reply("{\"result\":{}}\n").unwrap_err(),
            "the reply has no ok field"
        );
    }
}
