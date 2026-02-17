# 🐱 PrismCat

[English](./README.md) | [简体中文](./README_CN.md)

**PrismCat** is a lightweight, local-first **LLM API Transparent Proxy & Traffic Observability Tool** designed for developers.

Stop guessing what's happening behind your LLM SDKs. PrismCat lets you observe every byte sent to upstream providers, supports full streaming (SSE) logging, and provides a Postman-like **Replay** feature—all with zero code changes.

---

## ✨ Key Features

- 🚀 **Transparent Reverse Proxy**: Route-by-Subdomain (e.g., `openai.localhost`). Just change your `baseURL` and keep your SDKs as-is.
- 📊 **Full Traffic Observability**:
    - Complete request/response logging with pretty-printing for JSON and Text.
    - **SSE/Streaming Support**: Real-time logging of streaming responses without adding latency.
    - **Smart Base64 Folding**: Automatically collapses huge image Base64 strings in the UI to keep your logs clean.
- 🎮 **Developer Toolbox**: Built-in **Playground** for replaying requests, real-time stats dashboard, and full i18n support.
- 🔐 **Privacy & Security**:
    - Local-first storage using **SQLite**. No third-party servers involved.
    - Automatic sensitive header masking (`Authorization`, `api-key`).
- 📦 **High Performance**: Single-binary deployment with asynchronous log writing and automatic log retention/cleanup.

---

## 🛠️ Quick Start

### 1. Run Binary (Recommended)
Download the pre-compiled binary for your system from [Releases](https://github.com/paopaoandlingyia/PrismCat/releases).
- **Windows**: Run `prismcat.exe`. It will stay in your system tray. Right-click to open the dashboard.
- **Linux/macOS**: Run `./prismcat` in your terminal.

### 2. Run with Docker
```yaml
services:
  prismcat:
    image: ghcr.io/paopaoandlingyia/prismcat:latest
    container_name: prismcat
    ports:
      - "8080:8080"
    environment:
      - PRISMCAT_UI_PASSWORD=your_strong_password
      - PRISMCAT_RETENTION_DAYS=7
    volumes:
      - ./data:/app/data
    restart: always
```

---

## 🏗️ How it Works: Subdomain Routing

PrismCat uses **Subdomain Routing** to ensure maximum compatibility with any SDK.

**Example Scenario:**
Assume your upstream is named `openai` and your proxy domain is `localhost`:

Simply modify your `base_url` in your code:
```python
# OpenAI Python SDK Example
client = OpenAI(
    base_url="http://openai.localhost:8080/v1", # Pointing to PrismCat
    api_key="sk-..."
)
```

---

## 🛡️ License

[MIT License](LICENSE)
