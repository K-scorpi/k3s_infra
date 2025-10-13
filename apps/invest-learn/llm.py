# -----------------------------
# llm.py API
# -----------------------------
import requests
import os
from dotenv import load_dotenv
load_dotenv()

OPENROUTER_API_KEY = os.getenv("OPENROUTER_API_KEY")
def explain_signal_with_llm(ticker: str, metadata: dict) -> str:
    # Если недостаточно данных — не вызываем LLM
    if not metadata.get("price") or metadata.get("signal") == "HOLD" and not metadata.get("reasons"):
        return "Недостаточно данных для анализа."

    # Формируем детальный контекст для LLM
    context = f"Текущая цена: {metadata['price']:.2f} ₽\n"
    if metadata.get("sma_5") is not None:
        context += f"5-дневная SMA: {metadata['sma_5']:.2f} ₽\n"
    if metadata.get("sma_20") is not None:
        context += f"20-дневная SMA: {metadata['sma_20']:.2f} ₽\n"
    if metadata.get("rsi") is not None:
        context += f"RSI(14): {metadata['rsi']:.1f}\n"
    if metadata.get("macd") is not None and metadata.get("macd_signal") is not None:
        context += f"MACD: {metadata['macd']:.3f}, сигнал MACD: {metadata['macd_signal']:.3f}\n"

    prompt = f"""
Ты — профессиональный технический аналитик фондового рынка. Проанализируй текущую ситуацию по акции {ticker} на основе следующих данных:

{context}

Сигнал: {metadata['signal']}

Дай **чёткое, краткое и технически обоснованное** пояснение:
- Укажи, какие индикаторы подтверждают сигнал
- Объясни, что означает текущее положение цены относительно скользящих средних
- Интерпретируй значение RSI (перекупленность/перепроданность)
- Упомяни дивергенцию или подтверждение от MACD, если применимо

Ответ должен быть на русском языке, в 2–3 предложениях, без воды. Используй термины: SMA, RSI, MACD — инвестор их понимает.
"""

    YOUR_SITE_URL = "http://localhost"
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
                "temperature": 0.2,
                "max_tokens": 200
            },
            timeout=15
        )
        if response.status_code == 200:
            return response.json()["choices"][0]["message"]["content"].strip()
        else:
            return f"⚠️ LLM API error: {response.status_code}"
    except Exception as e:
        return f"⚠️ Не удалось получить пояснение: {e}"