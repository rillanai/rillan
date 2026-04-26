// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Deterministic outbound tokenizer seam. Mirrors `internal/tokenize` in the
//! Go repo. ADR-012.
//!
//! Token counts are pinned to an [`Encoding`] rather than a model alias so
//! they stay stable when upstream model names drift. Unknown models fall
//! back to a conservative byte/4 heuristic and emit a `tracing::warn` once
//! per (model | encoding) key.

use std::collections::HashMap;
use std::sync::{Arc, Mutex, OnceLock};

use thiserror::Error;
use tiktoken_rs::CoreBPE;
use tracing::warn;

/// Deterministic wire tokenization scheme.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Encoding {
    /// gpt-4 / gpt-4-turbo / gpt-3.5 family.
    CL100kBase,
    /// gpt-4o, gpt-4.1, gpt-5, o-series reasoning family.
    O200kBase,
}

impl Encoding {
    /// Returns the canonical lowercase wire string.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::CL100kBase => "cl100k_base",
            Self::O200kBase => "o200k_base",
        }
    }
}

/// Result of counting tokens for a single string.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct CountResult {
    /// Token count. Conservative round-up when `approximate` is true.
    pub tokens: usize,
    /// True when the backend could not produce an exact count and the
    /// heuristic fallback was used.
    pub approximate: bool,
    /// Encoding the exact count was produced with. None when approximate.
    pub encoding: Option<Encoding>,
}

/// Tokenizer trait. Implementations must be safe for concurrent use.
pub trait Counter: Send + Sync {
    fn count(&self, model: &str, text: &str) -> Result<CountResult, CountError>;
}

#[derive(Debug, Error)]
pub enum CountError {
    #[error("tokenize: load codec {0:?}: {1}")]
    LoadCodec(Encoding, String),
}

/// Returns the deterministic encoding for a bundled OpenAI-compatible model
/// name. Returns `None` when the model does not match a known family — the
/// caller should fall back to an approximate count.
#[must_use]
pub fn encoding_for_model(model: &str) -> Option<Encoding> {
    let name = model.trim().to_lowercase();
    if name.is_empty() {
        return None;
    }
    if name.starts_with("gpt-4o")
        || name.starts_with("gpt-4.1")
        || name.starts_with("gpt-5")
        || name.starts_with("o1")
        || name.starts_with("o3")
        || name.starts_with("o4")
    {
        return Some(Encoding::O200kBase);
    }
    if name.starts_with("gpt-4") || name.starts_with("gpt-3.5") {
        return Some(Encoding::CL100kBase);
    }
    None
}

/// Returns the default tiktoken-rs-backed counter. Codecs are lazily loaded
/// on first use and cached for the process lifetime. Warnings are emitted
/// once per (model | encoding) key via `tracing`.
pub fn new_counter() -> Arc<dyn Counter> {
    Arc::new(TiktokenCounter::default())
}

#[derive(Default)]
struct TiktokenCounter {
    codecs: Mutex<HashMap<Encoding, Arc<CoreBPE>>>,
    warned: Mutex<HashMap<String, ()>>,
}

impl Counter for TiktokenCounter {
    fn count(&self, model: &str, text: &str) -> Result<CountResult, CountError> {
        if text.is_empty() {
            return Ok(CountResult::default());
        }
        let Some(encoding) = encoding_for_model(model) else {
            self.warn_once(&format!("model:{}", model.trim().to_lowercase()), || {
                warn!(model = %model, "tokenize: unknown model, using approximate token count");
            });
            return Ok(CountResult {
                tokens: approximate_tokens(text),
                approximate: true,
                encoding: None,
            });
        };
        let codec = self.load_codec(encoding)?;
        let tokens = codec.encode_with_special_tokens(text).len();
        Ok(CountResult {
            tokens,
            approximate: false,
            encoding: Some(encoding),
        })
    }
}

impl TiktokenCounter {
    fn load_codec(&self, encoding: Encoding) -> Result<Arc<CoreBPE>, CountError> {
        let mut codecs = self.codecs.lock().expect("tokenize cache mutex poisoned");
        if let Some(codec) = codecs.get(&encoding) {
            return Ok(codec.clone());
        }
        let codec = match encoding {
            Encoding::CL100kBase => tiktoken_rs::cl100k_base(),
            Encoding::O200kBase => tiktoken_rs::o200k_base(),
        }
        .map_err(|err| CountError::LoadCodec(encoding, err.to_string()))?;
        let codec = Arc::new(codec);
        codecs.insert(encoding, codec.clone());
        Ok(codec)
    }

    fn warn_once<F: FnOnce()>(&self, key: &str, emit: F) {
        let mut warned = self.warned.lock().expect("warn dedupe mutex poisoned");
        if warned.contains_key(key) {
            return;
        }
        warned.insert(key.to_string(), ());
        emit();
    }
}

/// Sums the token cost of multiple texts measured against the same model.
/// Short-circuits on the first error. The aggregate is approximate if any
/// individual count was approximate, and carries the exact encoding used
/// when at least one exact count was produced.
pub fn count_strings(
    counter: &dyn Counter,
    model: &str,
    texts: &[&str],
) -> Result<CountResult, CountError> {
    let mut total = CountResult::default();
    for text in texts {
        let result = counter.count(model, text)?;
        total.tokens += result.tokens;
        if result.approximate {
            total.approximate = true;
        } else if result.encoding.is_some() {
            total.encoding = result.encoding;
        }
    }
    Ok(total)
}

/// Process-wide default counter. The HTTP layer reuses this for every request
/// rather than instantiating a fresh codec map per call.
pub fn shared_counter() -> Arc<dyn Counter> {
    static SHARED: OnceLock<Arc<dyn Counter>> = OnceLock::new();
    SHARED.get_or_init(new_counter).clone()
}

fn approximate_tokens(text: &str) -> usize {
    if text.is_empty() {
        return 0;
    }
    text.len().div_ceil(4)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn encoding_for_model_matches_go_table() {
        let cases: &[(&str, Option<Encoding>)] = &[
            ("gpt-4o", Some(Encoding::O200kBase)),
            ("gpt-4o-2024-08-06", Some(Encoding::O200kBase)),
            ("gpt-4o-mini", Some(Encoding::O200kBase)),
            ("gpt-4.1", Some(Encoding::O200kBase)),
            ("gpt-5-mini", Some(Encoding::O200kBase)),
            ("o1-preview", Some(Encoding::O200kBase)),
            ("o3-mini", Some(Encoding::O200kBase)),
            ("o4-mini", Some(Encoding::O200kBase)),
            ("gpt-4-turbo", Some(Encoding::CL100kBase)),
            ("gpt-4", Some(Encoding::CL100kBase)),
            ("gpt-3.5-turbo", Some(Encoding::CL100kBase)),
            ("  GPT-4O  ", Some(Encoding::O200kBase)),
            ("", None),
            ("claude-3-opus", None),
            ("gpt2", None),
        ];
        for (model, want) in cases {
            assert_eq!(encoding_for_model(model), *want, "model={model:?}",);
        }
    }

    #[test]
    fn counter_exact_for_bundled_encodings() {
        let counter = new_counter();
        let text = "Hello, Rillan tokenizer!";
        for (model, encoding) in [
            ("gpt-4o", Encoding::O200kBase),
            ("gpt-4-turbo", Encoding::CL100kBase),
            ("gpt-3.5-turbo", Encoding::CL100kBase),
        ] {
            let got = counter.count(model, text).expect("count");
            assert!(!got.approximate, "{model}: expected exact count");
            assert_eq!(got.encoding, Some(encoding), "{model}: encoding");
            assert!(got.tokens > 0, "{model}: positive token count");
            assert!(got.tokens <= text.len(), "{model}: tokens <= bytes");
        }
    }

    #[test]
    fn counter_exact_counts_differ_between_encodings() {
        let counter = new_counter();
        let text = "func main() { fmt.Println(\"hello\") }";
        let gpt4o = counter.count("gpt-4o", text).expect("gpt-4o");
        let gpt4 = counter.count("gpt-4-turbo", text).expect("gpt-4-turbo");
        assert!(!gpt4o.approximate && !gpt4.approximate);
        assert_ne!(gpt4o.encoding, gpt4.encoding);
    }

    #[test]
    fn counter_approximate_fallback_for_unknown_model() {
        let counter = new_counter();
        let text = "some untokenized text input";
        let got = counter.count("claude-3-opus", text).expect("count");
        assert!(got.approximate);
        assert!(got.encoding.is_none());
        let expected = text.len().div_ceil(4);
        assert_eq!(got.tokens, expected);
    }

    #[test]
    fn count_strings_aggregates_exact() {
        let counter = new_counter();
        let one = counter.count("gpt-4o", "hello").expect("one");
        let two = counter.count("gpt-4o", "world").expect("two");
        let counter_ref: &dyn Counter = counter.as_ref();
        let sum = count_strings(counter_ref, "gpt-4o", &["hello", "world"]).expect("sum");
        assert_eq!(sum.tokens, one.tokens + two.tokens);
        assert!(!sum.approximate);
        assert_eq!(sum.encoding, Some(Encoding::O200kBase));
    }

    #[test]
    fn count_strings_marks_approximate_when_any_fell_back() {
        let counter = new_counter();
        let counter_ref: &dyn Counter = counter.as_ref();
        let sum = count_strings(counter_ref, "claude-3-opus", &["hello", "world"]).expect("sum");
        assert!(sum.approximate);
        assert!(sum.encoding.is_none());
        assert!(sum.tokens > 0);
    }

    #[test]
    fn counter_empty_text_returns_zero() {
        let counter = new_counter();
        let got = counter.count("gpt-4o", "").expect("count");
        assert_eq!(got, CountResult::default());
    }
}
