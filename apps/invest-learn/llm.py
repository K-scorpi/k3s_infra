# -----------------------------
# llm.py API
# -----------------------------
import requests
import os
from dotenv import load_dotenv
load_dotenv()

OPENROUTER_API_KEY = os.getenv("OPENROUTER_API_KEY")
def explain_signal_with_llm(ticker: str, metadata: dict) -> str:
    if not metadata.get("reasons"):
        return "Недостаточно данных для анализа."

    reasons_str = "; ".join(metadata["reasons"])
    prompt = f"""
Ты — профессиональный финансовый аналитик. Объясни кратко и понятно для частного инвестора, почему по акции {ticker} сгенерирован сигнал "{metadata['signal']}".

Данные:
- Текущая цена: {metadata['price']:.2f} ₽
- Причины: {reasons_str}

Ответ дай на русском языке, в 1–2 предложениях, без жаргона. Не упоминай технические индикаторы напрямую — говори простыми словами.
"""
    YOUR_SITE_URL = "http://localhost"      # можно оставить
    YOUR_APP_NAME = "InvestBot"

    try:
        response = requests.post(
            url="https://openrouter.ai/api/v1/chat/completions",
            headers={
                "Authorization": f"Bearer {OPENROUTER_API_KEY}",
                "HTTP-Referer": YOUR_SITE_URL,
                "X-Title": YOUR_APP_NAME,
                "Content-Type": "application/json"
            },
            json={
                "model": "mistralai/mistral-7b-instruct",
                "messages": [{"role": "user", "content": prompt}],
                "temperature": 0.3,
                "max_tokens": 150
            },
            timeout=15
        )
        if response.status_code == 200:
            data = response.json()
            return data["choices"][0]["message"]["content"].strip()
        else:
            return f"⚠️ LLM API error: {response.status_code}"
    except Exception as e:
        return f"⚠️ Не удалось получить пояснение: {e}"