# importance-filter.md

Status: Draft  
Version: 0.1

# Importance Filter

## Purpose

The Importance Filter is responsible for determining whether an email deserves the user's attention.

The goal is not perfect classification.

The goal is reducing notification noise while minimizing the chance of missing important emails.

The filter combines:

- Static rules
- Extracted features
- User preferences
- Historical behavior
- Optional LLM classification

---

# Design Principles

## Explainable

Every classification should have a human-readable explanation.

Example:

text Importance: Important  Reason: - Sender belongs to school domain - Contains meeting-related keywords - User previously interacted with sender

---

## Privacy First

Most emails should be classified without using an LLM.

LLM providers should only be used when classification confidence is low.

---

## Learn Slowly

The system should never create strong rules after a single user action.

User feedback gradually changes weights.

---

## User Control

User-defined rules always override learned behavior.

Priority order:

text Explicit User Rules     ↓ Learned Rules     ↓ Built-in Rules     ↓ LLM Assistance

---

# Classification Categories

Supported categories:

text work finance legal government school family security travel shopping recruiting marketing social other

Additional categories may be added later.

---

# Email Lifecycle

text new     ↓ classified     ↓ notified     ↓ handled  or  ignored

Possible statuses:

text new classified important maybe ignored notified handled reply_needed

---

# Classification Pipeline

mermaid flowchart LR      Email[Email]      Headers[Header Extraction]     Structure[Structure Extraction]     Text[Text Extraction]     History[Historical Signals]      Features[Feature Set]      Rules[Rule Engine]      Decision{Confident?}      LLM[LLM Classification]      Final[Final Classification]      Email --> Headers     Email --> Structure     Email --> Text     Email --> History      Headers --> Features     Structure --> Features     Text --> Features     History --> Features      Features --> Rules      Rules --> Decision      Decision -->|Yes| Final     Decision -->|No| LLM     LLM --> Final

---

# Feature Extraction

The system extracts multiple groups of signals.

---

## Header Features

Extracted without reading email body.

text from_email from_domain reply_to to_me cc_me subject message_id in_reply_to references date list_unsubscribe precedence auto_submitted

Examples:

text List-Unsubscribe exists → likely newsletter  In-Reply-To exists → likely active conversation

---

## Structure Features

text attachment_count attachment_types attachment_names  has_calendar_invite  html_body_present plain_text_present  link_count body_size

Examples:

text Calendar invite → increase importance  PDF attachment → increase importance

---

## Text Features

Extracted using parsers and regular expressions.

Examples:

text dates deadlines amounts currencies urls emails phone numbers question count

Keyword groups:

text urgent meeting invoice payment interview security deadline school government

---

## Historical Features

Loaded from local SQLite database.

Examples:

text sender_seen_count  sender_reply_count  sender_marked_important_count  sender_marked_not_important_count  domain_importance_score  last_interaction_at  thread_activity

---

# Language Handling

The filter must support multiple languages.

Supported languages:

text English Polish Russian Romanian Italian

Additional languages may be added later.

---

# Language Detection Flow

mermaid flowchart LR      EmailText[Email Text]     Detector[Language Detector]     Language[Detected Language]      Language --> KeywordRules     Language --> LLMPrompt      KeywordRules[Language-Specific Rules]     LLMPrompt[Language-Aware Prompt]

---

# Keyword Dictionaries

Keywords should be grouped by meaning rather than language.

Example:

text invoice  ├─ invoice  ├─ payment  ├─ faktura  ├─ płatność  ├─ счет  ├─ оплата  ├─ factură  └─ fattura

The feature extractor produces normalized semantic features.

Example:

text invoice_keyword_detected = true

instead of storing individual language-specific keywords.

---

# Importance Scoring

The scoring engine calculates a score.

Example:

text sender_score + domain_score + keyword_score + history_score + thread_score + attachment_score  = total_score

---

# Classification Levels

text Critical Important Maybe Ignore

Example thresholds:

text 90+     Critical 70-89   Important 30-69   Maybe 0-29    Ignore

Exact thresholds may be tuned later.

---

# Rule Engine

Rules are applied before LLM classification.

Rule sources:

text built-in rules  user-defined rules  learned rules

---

## Built-in Rules

Examples:

text List-Unsubscribe → lower score  Security code → higher score  Calendar invite → higher score  Government domain → higher score

---

## User Rules

Examples:

text Always notify school.pl  Always notify teacher@school.pl  Ignore newsletter.example.com

User rules have highest priority.

---

## Learned Rules

Created from repeated user feedback.

Examples:

text User ignored 10 emails from domain  → decrease domain score  User replied to sender 8 times  → increase sender score

---

# User Feedback Learning

The system records every feedback event.

Examples:

text important  not important  handled  mute sender  always notify sender  always notify domain

---

# Learning Model

The system does not create hard rules immediately.

Instead it accumulates weights.

Example:

text not important  sender -= 10 domain -= 5

After multiple confirmations:

text sender -= 40  or  domain -= 30

The system gradually increases confidence.

---

# Feedback Learning Flow

mermaid sequenceDiagram      participant User     participant Telegram     participant Feedback     participant SQLite     participant RuleEngine      User->>Telegram: Mark Important      Telegram->>Feedback: Feedback Event      Feedback->>SQLite: Store Event      Feedback->>RuleEngine: Recalculate Weights      RuleEngine->>SQLite: Store Updated Scores

---

# LLM Integration

LLM classification is optional.

Used only when:

text score close to threshold  unknown sender  conflicting signals  insufficient history

---

# LLM Input

Should contain:

text minimal metadata  minimal body excerpt  privacy-filtered content

Depending on privacy settings.

---

# LLM Output

Expected structure:

json {   "importance": "important",   "category": "school",   "confidence": 0.91,   "reason": "Meeting request regarding child education",   "reply_needed": true }

---

# Main Classification Sequence

mermaid sequenceDiagram      participant Email     participant Extractor     participant RuleEngine     participant SQLite     participant LLM     participant Classifier      Email->>Extractor: Parse Email      Extractor->>Classifier: Features      Classifier->>RuleEngine: Score Features      RuleEngine->>SQLite: Load Rules & History     SQLite-->>RuleEngine: Rules & Feedback      RuleEngine-->>Classifier: Score      alt Confident         Classifier-->>Classifier: Final Decision     else Uncertain         Classifier->>LLM: Request Classification         LLM-->>Classifier: Classification Result     end

---

# Database Schema

## Entity Relationship Diagram

mermaid erDiagram      EMAILS ||--o{ CLASSIFICATIONS : has     EMAILS ||--o{ FEEDBACK_EVENTS : receives      SENDERS ||--o{ EMAILS : sends     DOMAINS ||--o{ SENDERS : owns      SENDERS ||--o{ SENDER_PREFERENCES : has     DOMAINS ||--o{ DOMAIN_PREFERENCES : has      EMAILS {         string id         string sender_id         string subject         string language         datetime received_at     }      SENDERS {         string id         string email         int importance_score     }      DOMAINS {         string id         string domain         int importance_score     }      CLASSIFICATIONS {         string id         string email_id         string category         string importance         float confidence     }      FEEDBACK_EVENTS {         string id         string email_id         string action         datetime created_at     }      SENDER_PREFERENCES {         string sender_id         int score     }      DOMAIN_PREFERENCES {         string domain_id         int score     }

---

# Example Classifications

## Example 1

text From: teacher@school.pl  Subject: Spotkanie WOPFU

Signals:

text school domain meeting keyword previous interactions

Result:

text Important

---

## Example 2

text From: newsletter@example.com  Subject: Summer Sale

Signals:

text List-Unsubscribe marketing keywords low interaction history

Result:

text Ignore

---

# Future Improvements

Potential future enhancements:

text Sender reputation scoring  Thread-level importance  Per-account rules  Time-based weighting  Personal schedule awareness  Document classification  Calendar integration  Semantic sender clustering

These improvements are intentionally outside the MVP scope.