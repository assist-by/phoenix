```mermaid
flowchart TD
    subgraph Main[Trading Bot Main]
        Config[설정 관리]
        Scheduler[스케줄러]
    end

    subgraph Market[시장 데이터]
        Binance[바이낸스 API 클라이언트]
        DataCollector[데이터 수집기]
    end

    subgraph Analysis[기술적 분석]
        Calculator[지표 계산]
        SignalDetector[시그널 감지]
    end

    subgraph Trading[거래 실행]
        OrderManager[주문 관리]
        PositionManager[포지션 관리]
    end

    subgraph Notification[알림]
        Discord[디스코드 클라이언트]
        MessageFormatter[메시지 포맷터]
    end

    Config --> Scheduler
    Scheduler --> DataCollector
    DataCollector --> Binance
    DataCollector --> Calculator
    Calculator --> SignalDetector
    SignalDetector --> OrderManager
    OrderManager --> Binance
    OrderManager --> Discord
    PositionManager --> OrderManager
    SignalDetector --> MessageFormatter
    MessageFormatter --> Discord
```