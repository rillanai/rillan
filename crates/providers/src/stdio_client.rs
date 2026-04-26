// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Out-of-process stdio provider adapter. Mirrors
//! `internal/providers/stdio/client.go`.
//!
//! Spawns the configured command, writes the request envelope as JSON on
//! stdin, and reads a JSON response envelope from stdout.

use std::collections::HashMap;
use std::process::Stdio;

use async_trait::async_trait;
use bytes::Bytes;
use http::header::HeaderName;
use http::{HeaderMap, HeaderValue, StatusCode};
use rillan_chat::{ProviderRequest, Request};
use serde::{Deserialize, Serialize};
use tokio::io::AsyncWriteExt;
use tokio::process::Command;

use crate::{Provider, ProviderBody, ProviderError, ProviderResponse};

#[derive(Debug, Clone)]
pub struct StdioProvider {
    command: Vec<String>,
}

impl StdioProvider {
    /// Builds the adapter from a non-empty command.
    #[must_use]
    pub fn new(command: Vec<String>) -> Self {
        Self { command }
    }
}

#[async_trait]
impl Provider for StdioProvider {
    fn name(&self) -> &str {
        "stdio"
    }

    async fn ready(&self) -> Result<(), ProviderError> {
        if self.command.is_empty() {
            return Err(ProviderError::Stdio(
                "stdio provider command must not be empty".into(),
            ));
        }
        if which::which(&self.command[0]).is_err() {
            return Err(ProviderError::Stdio(format!(
                "resolve stdio provider command: {:?}",
                self.command[0]
            )));
        }
        Ok(())
    }

    async fn chat_completions(
        &self,
        request: ProviderRequest,
    ) -> Result<ProviderResponse, ProviderError> {
        if request.request.stream {
            return Err(ProviderError::Stdio(
                "stdio provider does not support streaming responses".into(),
            ));
        }
        self.ready().await?;

        let raw_body = if request.raw_body.is_empty() {
            serde_json::Value::Null
        } else {
            serde_json::from_slice::<serde_json::Value>(&request.raw_body).map_err(|err| {
                ProviderError::Stdio(format!("encode stdio provider request: {err}"))
            })?
        };
        let envelope = ChatCompletionRequestEnvelope {
            request: request.request.clone(),
            raw_body,
        };
        let payload = serde_json::to_vec(&envelope).map_err(ProviderError::MarshalPayload)?;

        let mut child = Command::new(&self.command[0])
            .args(&self.command[1..])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|err| ProviderError::Stdio(format!("spawn stdio provider: {err}")))?;

        if let Some(mut stdin) = child.stdin.take() {
            stdin
                .write_all(&payload)
                .await
                .map_err(|err| ProviderError::Stdio(format!("write stdio request: {err}")))?;
        }

        let output = child
            .wait_with_output()
            .await
            .map_err(|err| ProviderError::Stdio(format!("await stdio provider: {err}")))?;

        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
            if stderr.is_empty() {
                return Err(ProviderError::Stdio(format!(
                    "run stdio provider failed: {}",
                    output.status,
                )));
            }
            return Err(ProviderError::Stdio(format!(
                "run stdio provider failed: {}: {stderr}",
                output.status,
            )));
        }
        if output.stdout.is_empty() {
            return Err(ProviderError::Stdio(
                "read stdio provider response: empty stdout".into(),
            ));
        }

        let response_envelope: ChatCompletionResponseEnvelope =
            serde_json::from_slice(&output.stdout)
                .map_err(|err| ProviderError::Stdio(format!("decode stdio response: {err}")))?;

        let mut status_code = response_envelope.status_code;
        if status_code == 0 {
            status_code = 200;
        }
        let status = StatusCode::from_u16(status_code).map_err(|_| {
            ProviderError::Stdio(format!(
                "decode stdio response: invalid status_code {status_code}"
            ))
        })?;

        let mut headers = HeaderMap::new();
        for (key, values) in response_envelope.headers {
            let Ok(name) = key.parse::<HeaderName>() else {
                continue;
            };
            for value in values {
                if let Ok(header_value) = HeaderValue::from_str(&value) {
                    headers.append(&name, header_value);
                }
            }
        }
        if !headers.contains_key(http::header::CONTENT_TYPE) {
            headers.insert(
                http::header::CONTENT_TYPE,
                HeaderValue::from_static("application/json"),
            );
        }

        Ok(ProviderResponse {
            status,
            headers,
            body: ProviderBody::Buffered(Bytes::from(
                serde_json::to_vec(&response_envelope.body)
                    .map_err(ProviderError::MarshalPayload)?,
            )),
        })
    }
}

#[derive(Debug, Serialize)]
struct ChatCompletionRequestEnvelope {
    request: Request,
    raw_body: serde_json::Value,
}

#[derive(Debug, Deserialize)]
struct ChatCompletionResponseEnvelope {
    #[serde(default)]
    status_code: u16,
    #[serde(default)]
    headers: HashMap<String, Vec<String>>,
    #[serde(default)]
    body: serde_json::Value,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn ready_rejects_empty_command() {
        let provider = StdioProvider::new(Vec::new());
        let err = provider.ready().await.expect_err("empty command");
        assert!(matches!(err, ProviderError::Stdio(_)));
    }

    #[tokio::test]
    async fn ready_rejects_missing_command() {
        let provider =
            StdioProvider::new(vec!["definitely-missing-rillan-stdio-provider".to_string()]);
        let err = provider.ready().await.expect_err("missing command");
        assert!(matches!(err, ProviderError::Stdio(_)));
    }

    // Shell-script fixtures only run on unix targets; the Go suite skips on
    // windows for the same reason.
    #[cfg(unix)]
    mod unix_fixtures {
        use super::*;
        use std::os::unix::fs::PermissionsExt;
        use std::path::PathBuf;

        fn write_executable_script(dir: &std::path::Path, content: &str) -> PathBuf {
            let path = dir.join("provider.sh");
            std::fs::write(&path, content).expect("write script");
            let mut perms = std::fs::metadata(&path).expect("metadata").permissions();
            perms.set_mode(0o755);
            std::fs::set_permissions(&path, perms).expect("chmod");
            path
        }

        fn provider_request() -> ProviderRequest {
            ProviderRequest {
                request: Request {
                    model: "demo-model".into(),
                    ..Request::default()
                },
                raw_body: bytes::Bytes::from_static(b"{\"model\":\"demo-model\"}"),
            }
        }

        #[tokio::test]
        async fn runs_command_and_returns_synthetic_response() {
            let tmp = tempfile::tempdir().unwrap();
            let request_path = tmp.path().join("request.json");
            let script = write_executable_script(
                tmp.path(),
                &format!(
                    "#!/bin/sh\nset -eu\ncat > {request}\nprintf '%s' '{response}'\n",
                    request = shell_quote(request_path.to_str().unwrap()),
                    response = "{\"status_code\":200,\"headers\":{\"Content-Type\":[\"application/json\"]},\"body\":{\"id\":\"resp_123\"}}",
                ),
            );

            let provider = StdioProvider::new(vec![script.to_string_lossy().into_owned()]);
            let response = provider
                .chat_completions(provider_request())
                .await
                .expect("chat_completions");
            assert_eq!(response.status, http::StatusCode::OK);
            let body = response.body.collect().await.expect("collect body");
            assert_eq!(&body[..], br#"{"id":"resp_123"}"#);

            let written = std::fs::read_to_string(&request_path).expect("read request");
            assert_eq!(
                written,
                r#"{"request":{"model":"demo-model","messages":[]},"raw_body":{"model":"demo-model"}}"#,
            );
        }

        #[tokio::test]
        async fn surfaces_stderr_on_failure() {
            let tmp = tempfile::tempdir().unwrap();
            let script = write_executable_script(tmp.path(), "#!/bin/sh\necho boom >&2\nexit 3\n");
            let provider = StdioProvider::new(vec![script.to_string_lossy().into_owned()]);
            let err = provider
                .chat_completions(provider_request())
                .await
                .expect_err("must fail");
            let message = match err {
                ProviderError::Stdio(msg) => msg,
                other => panic!("expected Stdio error, got {other:?}"),
            };
            assert!(message.contains("boom"), "stderr missing from {message:?}");
        }

        #[tokio::test]
        async fn rejects_invalid_status_code() {
            let tmp = tempfile::tempdir().unwrap();
            let script = write_executable_script(
                tmp.path(),
                "#!/bin/sh\nprintf '%s' '{\"status_code\":42,\"body\":{\"id\":\"resp_123\"}}'\n",
            );
            let provider = StdioProvider::new(vec![script.to_string_lossy().into_owned()]);
            let err = provider
                .chat_completions(provider_request())
                .await
                .expect_err("must fail");
            assert!(matches!(err, ProviderError::Stdio(_)));
        }

        // Single-quote a path for embedding in a `/bin/sh` script. Wraps in
        // single quotes and escapes any embedded quote characters.
        fn shell_quote(value: &str) -> String {
            let escaped = value.replace('\'', "'\\''");
            format!("'{escaped}'")
        }
    }
}
