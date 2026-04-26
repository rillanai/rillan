// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Local intent classifier. Mirrors `internal/classify/classifier.go`.
//! ADR-011 — graceful degradation: a [`SafeClassifier`] swallows errors and
//! returns `None` so the rest of the request pipeline continues normally.

use async_trait::async_trait;
use rillan_chat::{message_text, Request};
use rillan_policy::{ActionType, ExecutionMode, IntentClassification, Sensitivity};
use serde::Deserialize;
use thiserror::Error;

const DEFAULT_MIN_CONFIDENCE: f64 = 0.5;

const CLASSIFY_PROMPT: &str = "Classify the following chat request for a local policy engine. Return only JSON with these fields:
- action_type: one of code_diagnosis, code_generation, architecture, explanation, refactor, review, general_qa
- sensitivity: one of public, internal, proprietary, trade_secret
- requires_context: boolean
- execution_mode: one of direct, plan_first
- confidence: number from 0 to 1

Request:
{input}";

/// Errors raised by classifier implementations.
#[derive(Debug, Error)]
pub enum Error {
    #[error("classifier response did not contain JSON object")]
    NoJsonObject,
    #[error("parse classifier response: {0}")]
    Parse(#[source] serde_json::Error),
    #[error("invalid action_type {0:?}")]
    InvalidActionType(String),
    #[error("invalid sensitivity {0:?}")]
    InvalidSensitivity(String),
    #[error("invalid execution_mode {0:?}")]
    InvalidExecutionMode(String),
    #[error("classification confidence below threshold")]
    LowConfidence,
    #[error("read message content: {0}")]
    ReadMessage(#[source] rillan_chat::ValidateError),
    #[error("ollama: {0}")]
    Ollama(#[from] rillan_ollama::Error),
}

/// Returns true when the error is the low-confidence sentinel.
#[must_use]
pub fn is_low_confidence(err: &Error) -> bool {
    matches!(err, Error::LowConfidence)
}

/// Async classifier trait.
#[async_trait]
pub trait Classifier: Send + Sync {
    async fn classify(&self, req: &Request) -> Result<IntentClassification, Error>;
}

/// Ollama-backed classifier.
#[derive(Debug, Clone)]
pub struct OllamaClassifier {
    client: rillan_ollama::Client,
    model: String,
    min_confidence: f64,
}

impl OllamaClassifier {
    pub fn new(client: rillan_ollama::Client, model: impl Into<String>) -> Self {
        Self {
            client,
            model: model.into(),
            min_confidence: DEFAULT_MIN_CONFIDENCE,
        }
    }
}

#[async_trait]
impl Classifier for OllamaClassifier {
    async fn classify(&self, req: &Request) -> Result<IntentClassification, Error> {
        let input = build_input(req)?;
        let prompt = CLASSIFY_PROMPT.replace("{input}", &input);
        let response = self.client.generate(&self.model, &prompt).await?;
        let classification = parse_classification(&response)?;
        if classification.confidence < self.min_confidence {
            return Err(Error::LowConfidence);
        }
        Ok(classification)
    }
}

/// Wraps an inner classifier and converts every error into a `None` result.
pub struct SafeClassifier {
    inner: Option<Box<dyn Classifier>>,
}

impl std::fmt::Debug for SafeClassifier {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("SafeClassifier")
            .field("has_inner", &self.inner.is_some())
            .finish()
    }
}

impl SafeClassifier {
    #[must_use]
    pub fn new(inner: Option<Box<dyn Classifier>>) -> Self {
        Self { inner }
    }

    /// Returns `None` when the inner classifier errored or is absent.
    pub async fn classify(&self, req: &Request) -> Option<IntentClassification> {
        let inner = self.inner.as_ref()?;
        inner.classify(req).await.ok()
    }
}

fn build_input(req: &Request) -> Result<String, Error> {
    let mut parts = Vec::with_capacity(req.messages.len());
    for message in &req.messages {
        let content = message_text(message).map_err(Error::ReadMessage)?;
        parts.push(format!("{}: {}", message.role, content.trim()));
    }
    Ok(parts.join("\n").trim().to_string())
}

fn parse_classification(response: &str) -> Result<IntentClassification, Error> {
    let start = response.find('{').ok_or(Error::NoJsonObject)?;
    let end = response.rfind('}').ok_or(Error::NoJsonObject)?;
    if end < start {
        return Err(Error::NoJsonObject);
    }
    let raw: RawClassification =
        serde_json::from_str(&response[start..=end]).map_err(Error::Parse)?;

    let action = parse_action_type(&raw.action_type)?;
    let sensitivity = parse_sensitivity(&raw.sensitivity)?;
    let execution_mode = parse_execution_mode(&raw.execution_mode)?;

    Ok(IntentClassification {
        action,
        sensitivity,
        requires_context: raw.requires_context,
        execution_mode,
        confidence: raw.confidence,
    })
}

fn parse_action_type(value: &str) -> Result<ActionType, Error> {
    match value.trim() {
        "code_diagnosis" => Ok(ActionType::CodeDiagnosis),
        "code_generation" => Ok(ActionType::CodeGeneration),
        "architecture" => Ok(ActionType::Architecture),
        "explanation" => Ok(ActionType::Explanation),
        "refactor" => Ok(ActionType::Refactor),
        "review" => Ok(ActionType::Review),
        "general_qa" => Ok(ActionType::GeneralQa),
        other => Err(Error::InvalidActionType(other.to_string())),
    }
}

fn parse_sensitivity(value: &str) -> Result<Sensitivity, Error> {
    match value.trim() {
        "public" => Ok(Sensitivity::Public),
        "internal" => Ok(Sensitivity::Internal),
        "proprietary" => Ok(Sensitivity::Proprietary),
        "trade_secret" => Ok(Sensitivity::TradeSecret),
        other => Err(Error::InvalidSensitivity(other.to_string())),
    }
}

fn parse_execution_mode(value: &str) -> Result<ExecutionMode, Error> {
    match value.trim() {
        "direct" => Ok(ExecutionMode::Direct),
        "plan_first" => Ok(ExecutionMode::PlanFirst),
        other => Err(Error::InvalidExecutionMode(other.to_string())),
    }
}

#[derive(Debug, Deserialize, Default)]
#[serde(default)]
struct RawClassification {
    action_type: String,
    sensitivity: String,
    requires_context: bool,
    execution_mode: String,
    confidence: f64,
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use serde_json::Value;
    use std::collections::BTreeMap;
    use wiremock::matchers::{method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    fn user_request() -> Request {
        Request {
            model: "gpt-4o-mini".into(),
            messages: vec![rillan_chat::Message {
                role: "user".into(),
                content: Value::String("please review this diff".into()),
                extra: BTreeMap::new(),
            }],
            ..Request::default()
        }
    }

    async fn mock_generate(server: &MockServer, response_text: &str) {
        Mock::given(method("POST"))
            .and(path("/api/generate"))
            .respond_with(
                ResponseTemplate::new(200).set_body_json(json!({"response": response_text})),
            )
            .mount(server)
            .await;
    }

    #[tokio::test]
    async fn parses_valid_response() {
        let server = MockServer::start().await;
        mock_generate(
            &server,
            r#"{"action_type":"code_generation","sensitivity":"internal","requires_context":true,"execution_mode":"plan_first","confidence":0.92}"#,
        )
        .await;
        let classifier = OllamaClassifier::new(rillan_ollama::Client::new(server.uri()), "qwen");
        let got = classifier
            .classify(&user_request())
            .await
            .expect("classify");
        assert_eq!(got.action, ActionType::CodeGeneration);
        assert_eq!(got.sensitivity, Sensitivity::Internal);
        assert!(got.requires_context);
        assert_eq!(got.execution_mode, ExecutionMode::PlanFirst);
        assert!((got.confidence - 0.92).abs() < 1e-6);
    }

    #[tokio::test]
    async fn rejects_malformed_json_payload() {
        let server = MockServer::start().await;
        mock_generate(&server, "not json").await;
        let classifier = OllamaClassifier::new(rillan_ollama::Client::new(server.uri()), "qwen");
        let err = classifier.classify(&user_request()).await.expect_err("err");
        assert!(matches!(err, Error::NoJsonObject));
    }

    #[tokio::test]
    async fn rejects_partial_response() {
        let server = MockServer::start().await;
        mock_generate(&server, r#"{"action_type":"review"}"#).await;
        let classifier = OllamaClassifier::new(rillan_ollama::Client::new(server.uri()), "qwen");
        let err = classifier.classify(&user_request()).await.expect_err("err");
        assert!(matches!(err, Error::InvalidSensitivity(_)));
    }

    #[tokio::test]
    async fn rejects_low_confidence_response() {
        let server = MockServer::start().await;
        mock_generate(
            &server,
            r#"{"action_type":"review","sensitivity":"public","requires_context":false,"execution_mode":"direct","confidence":0.2}"#,
        )
        .await;
        let classifier = OllamaClassifier::new(rillan_ollama::Client::new(server.uri()), "qwen");
        let err = classifier.classify(&user_request()).await.expect_err("err");
        assert!(is_low_confidence(&err));
    }

    struct FailingClassifier;

    #[async_trait]
    impl Classifier for FailingClassifier {
        async fn classify(&self, _req: &Request) -> Result<IntentClassification, Error> {
            Err(Error::LowConfidence)
        }
    }

    #[tokio::test]
    async fn safe_classifier_suppresses_errors() {
        let safe = SafeClassifier::new(Some(Box::new(FailingClassifier)));
        let got = safe.classify(&user_request()).await;
        assert!(got.is_none());
    }
}
