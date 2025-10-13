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
    if not metadata.get("reasons"):
        return "Недостаточно данных для анализа."

    # Формируем детальный контекст для LLM
    lines = [f"Текущая цена: {metadata['price']:.2f} ₽"]
    if metadata.get("sma_20") is not None:
        lines.append(f"20-дневная SMA: {metadata['sma_20']:.2f} ₽")
    if metadata.get("rsi") is not None:
        lines.append(f"RSI(14): {metadata['rsi']:.1f}")
    if metadata.get("macd") is not None and metadata.get("macd_signal") is not None:
        lines.append(f"MACD: {metadata['macd']:.4f}, сигнальная линия: {metadata['macd_signal']:.4f}")
    if metadata.get("bb_high") is not None:
        lines.append(f"Полосы Боллинджера: верхняя {metadata['bb_high']:.2f}, нижняя {metadata['bb_low']:.2f}")
    if metadata.get("atr") is not None:
        lines.append(f"ATR(14): {metadata['atr']:.2f} — мера волатильности")

    context = "\n".join(lines)

    prompt = f"""
Ты — профессиональный технический аналитик MOEX. Проанализируй акцию {ticker} на основе данных:

{context}

Сигнал: {metadata['signal']}

Дай **глубокий, структурированный и практичный** анализ:
1. Опиши текущую рыночную ситуацию.
2. Перечисли, какие индикаторы подтверждают сигнал, а какие противоречат.
3. Упомяни дивергенции, пробои, зоны перекупленности/перепроданности.
4. Дай рекомендацию: покупать, продавать или держать — и почему.
5. Укажи ключевые уровни поддержки/сопротивления (например, SMA20, полосы Боллинджера).

Ответ на русском, 3–5 предложений. Используй термины: SMA, RSI, MACD, Bollinger Bands, ATR — инвестор их понимает.
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