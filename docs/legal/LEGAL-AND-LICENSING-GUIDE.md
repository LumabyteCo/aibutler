# Legal & Licensing Guide for AI Butler

**Date:** 2026-02-21
**Disclaimer:** This document is informational and does not constitute legal advice. Consult a technology/IP lawyer before launching.

---

## 1. SDK Licenses Summary

| Provider | SDK License | Can Redistribute? | Can Build Commercial? | Can Build Competing AI? |
|----------|-----------|-------------------|----------------------|------------------------|
| **Anthropic (Claude)** | MIT | Yes | Yes (via API) | No |
| **OpenAI** | Apache 2.0 | Yes | Yes (via API) | No |
| **Google (Gemini)** | Apache 2.0 | Yes | Yes (via API) | No |

All three SDKs are permissively licensed. AI Butler can freely use, modify, and redistribute SDK code.

---

## 2. Anthropic / Claude SDK

### License
- **MIT License** (Copyright 2023 Anthropic, PBC)
- Repository: [anthropics/anthropic-sdk-python](https://github.com/anthropics/anthropic-sdk-python)
- Claude Agent SDK is also MIT licensed

### Critical Distinction
**Anthropic's proprietary coding agent is proprietary** -- "All rights reserved. Use is subject to Anthropic's Commercial Terms of Service." This is NOT the same as the SDK.

### Terms of Service Restrictions (Section D.4)
- **(a)** Do NOT build a competing product or service, train competing AI models, or resell Services
- **(b)** Do NOT reverse engineer the Services
- **(c)** Do NOT support third parties in the above

### Recent Enforcement (Aggressive)
- **Jan 9, 2026:** Deployed server-side checks blocking ALL third-party tools from using Claude Pro/Max OAuth tokens
- **Aug 2025:** Revoked OpenAI's API access for using it to benchmark GPT-5
- **Jan 2026:** Blocked xAI from using Claude through Cursor IDE
- **Jan 2026:** Blocked third-party "harnesses" like OpenCode from using consumer tokens

### Key Rule for AI Butler
**Consumer subscription tokens (OAuth from Claude Pro/Max) are PROHIBITED in third-party tools.** Only **API keys from Claude Console** are permitted.

### Trademark
- "Claude" and "Anthropic" are trademarks
- Cannot use in product names
- Descriptive use ("supports Claude API") is acceptable under nominative fair use

---

## 3. OpenAI SDK

### License
- **Apache License 2.0** (Copyright 2026 OpenAI)
- Provides explicit patent grant (unlike MIT)

### Terms of Service Restrictions
- Cannot use Output to develop AI models competing with OpenAI (except permitted exceptions like small classifiers)
- Cannot buy, sell, or transfer API keys
- Cannot reverse engineer Services

### Trademark (Strictly Enforced)
- **"GPT" CANNOT be used in product names** -- OpenAI sends legal letters
- **"ChatGPT" CANNOT be used in product names**
- **"OpenAI" name and logo** are protected
- Model names (GPT-4, GPT-5) are NOT permitted in app titles
- Acceptable: "Powered by OpenAI" badge (for active API customers)
- Use "powered by" or "built on" -- NOT "built with" or "partnered with"

---

## 4. Google Gemini SDK

### License
- **Apache License 2.0** (Google LLC)
- New unified SDK: [googleapis/python-genai](https://github.com/googleapis/python-genai) (GA May 2025)
- Old SDK deprecated November 30, 2025

### Terms of Service Restrictions
- Cannot develop models that compete with Gemini API or Google AI Studio
- Cannot reverse engineer Services
- Cannot bypass safety measures
- Cannot use in clinical practice
- EEA/Switzerland/UK users MUST use paid tier

### Data Usage Warning
- **Free tier:** Google uses prompts/responses to improve products; human reviewers may read them
- **Paid tier:** Google does NOT use prompts/responses to improve products

### Trademark
- "Gemini" is a Google trademark
- Standard Google brand guidelines apply
- May reference descriptively ("supports Gemini API")

---

## 5. The Gateway Pattern: Legal Precedent

Building an open-source AI gateway/proxy that routes requests to multiple LLM providers is a **well-established and legally viable pattern:**

| Project | License | Status |
|---------|---------|--------|
| LiteLLM (BerriAI) | MIT | Active, YC-backed, 100+ providers |
| Portkey | Enterprise/OSS hybrid | Active, enterprise-focused |
| LLM Gateway | AGPLv3 | Active, self-hosted |

These projects have operated for years without legal action, establishing strong precedent that **middleware/gateway layers are acceptable.**

---

## 6. What IS Legal for AI Butler

1. **BYOK (Bring Your Own Key) model** -- Users provide their own API keys. AI Butler routes and manages requests. Zero legal risk.
2. **Using SDK code** -- All three SDKs are permissively licensed
3. **Routing API requests** -- You are middleware, not a competing AI
4. **Descriptive references** -- "Supports Claude API, OpenAI API, and Gemini API"

---

## 7. What is NOT Legal

1. **Reselling API access via consumer subscriptions** -- Using personal Claude Pro/Max or ChatGPT Plus accounts to provide API access
2. **Training competing models** -- Using any provider's output to train a competing LLM
3. **Trademark misuse** -- Naming the product "Claude Router," "GPT Gateway," or "Gemini Proxy"
4. **API key resale** -- Buying, selling, or transferring API keys
5. **Caching responses** (potentially) -- Unless your privacy policy and ToS allow it

---

## 8. Recommended Legal Architecture for AI Butler

### 1. Project License
**Apache 2.0** -- chosen over MIT for specific reasons.

#### Why Apache 2.0 (Not MIT)

| Aspect | MIT | Apache 2.0 | Impact for AI Butler |
|--------|-----|-----------|-----------------|
| Patent grant | **None** | **Explicit grant from contributors** | If a contributor's employer patents a technique used in AI Butler, Apache 2.0 protects all users. MIT wouldn't. |
| Patent retaliation | **None** | **License terminates if you sue users for patents** | Discourages patent trolls from attacking the project. |
| Enterprise perception | "Simple but no patent safety" | "Industry standard, patent-safe" | Enterprise legal teams reviewing AI Butler for adoption prefer explicit patent clause. |
| SDK compatibility | Compatible with Apache 2.0 (must carry NOTICE) | Natively compatible | OpenAI SDK and Gemini SDK are Apache 2.0. Clean inclusion. |
| Industry alignment | Libraries and small tools | Infrastructure projects | Kubernetes, TensorFlow, Android, OpenAI SDK all use Apache 2.0. |
| Extra requirements | Copyright notice only | Copyright notice + NOTICE file + mark modified files | Trivial extra work (~10 minutes) for significant patent protection. |
| GPLv2 compatibility | Yes | No (but compatible with GPLv3) | Not relevant -- AI Butler is a standalone application, not a library embedded in GPL projects. |

**Decision: Apache 2.0.** The patent protection is critical for a security-focused project targeting enterprise adoption.

**Note on the multi-channel framework:** Both the multi-channel framework and AI Butler are open source. Apache 2.0 is NOT a differentiator against the multi-channel framework. It IS a differentiator against the source-available terminal tool (FSL-1.1-MIT, which restricts competing use for 2 years and is not truly open source by OSI standards).

### 2. Authentication Model
**BYOK (Bring Your Own Key)** by default. Users provide their own API keys for each provider. This applies to cloud LLM providers, messaging platform tokens, and IoT platform tokens (e.g., Home Assistant).

### 3. Project Name
- "AI Butler" -- unique, not trademarked in AI space
- Do NOT include "Claude," "GPT," "Gemini," "OpenAI," or "Anthropic" in the name
- In documentation, use nominative fair use: "Supports Claude API"

### 4. Disclaimers Required
Include in README and documentation:
```
AI Butler is not affiliated with, endorsed by, or sponsored by Anthropic, OpenAI,
Google, or any other LLM provider. All trademarks belong to their respective
owners. Users are responsible for complying with the terms of service of their
chosen LLM providers.
```

### 5. Attribution Requirements
- Include MIT copyright notice for Anthropic SDK code
- Include Apache 2.0 license and NOTICE file for OpenAI/Google SDK code
- Maintain NOTICE file listing all third-party attributions

### 6. Contributor Agreement
Consider a Contributor License Agreement (CLA) or Developer Certificate of Origin (DCO) to ensure clean IP chain. DCO is lighter weight and may encourage more contributions.

---

## 9. Regulatory Considerations

- **EU AI Act** (effective August 2026): Requires documentation of AI system components for high-risk systems. AI Butler's audit logging and capability documentation support compliance.
- **February 2025 precedent:** A startup settled a **$375,000 lawsuit** for using a "research-only" licensed model in production
- **34% of open-source LLMs** on Hugging Face now use custom licenses (up from 12% in 2023) -- always check individual model licenses
- **IoT regulatory landscape:** Smart home device control through AI agents may fall under consumer protection regulations in some jurisdictions. The three-tier security model helps demonstrate due diligence.

---

## 10. Action Items for AI Butler

- [ ] Register "AI Butler" as a trademark (search first)
- [ ] Adopt Apache 2.0 license (confirmed -- patent protection is the key reason)
- [ ] Create NOTICE file for third-party attributions
- [ ] Implement BYOK authentication model
- [ ] Add provider disclaimers to README
- [ ] Include proper SDK attribution (MIT for Anthropic, Apache 2.0 for OpenAI/Google)
- [ ] Draft Terms of Service for any hosted components (WebChat, web dashboard)
- [ ] Set up alerts for ToS changes from all three major providers
- [ ] Choose CLA vs DCO for contributors (DCO recommended for lower friction)
- [ ] Consult IP lawyer before public launch
- [ ] Review IoT regulatory requirements in target markets

---

## Sources

- [Anthropic SDK License](https://github.com/anthropics/anthropic-sdk-python)
- Proprietary Coding Agent: FSL-1.1-MIT (source-available, not open source by OSI standards)
- [Anthropic ToS Crackdown - VentureBeat](https://venturebeat.com/technology/anthropic-cracks-down-on-unauthorized-claude-usage-by-third-party-harnesses)
- [OpenAI Python SDK License](https://github.com/openai/openai-python)
- [OpenAI Services Agreement](https://openai.com/policies/services-agreement/)
- [OpenAI Brand Guidelines](https://openai.com/brand/)
- [OpenAI GPT Trademark Enforcement - Slator](https://slator.com/openai-hunts-down-companies-using-trademarked-gpt-in-brand/)
- [Google GenAI SDK License](https://github.com/googleapis/python-genai)
- [Gemini API Terms of Service](https://ai.google.dev/gemini-api/terms)
- [Google Brand Resource Center](https://about.google/brand-resource-center/guidance/)
- [LiteLLM - GitHub](https://github.com/BerriAI/litellm)
- [Open-Source LLM Licensing Risks](https://brics-econ.org/open-source-llm-licensing-what-you-must-know-to-avoid-legal-risks)
