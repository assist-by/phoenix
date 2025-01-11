# phoenix

## 매매 알고리즘

```mermaid
flowchart TD
    Start([시작]) --> Init[초기 설정:\n- MACD\n- Parabolic SAR\n- 200 EMA]
    Init --> CheckPrice{가격이\n200 EMA 기준\n위/아래?}
    
    %% Long Position Flow
    CheckPrice -->|위| CheckLong{MACD Line이\nSignal Line을\n상향돌파?}
    CheckLong -->|Yes| CheckSARLong{Parabolic SAR가\n캔들 아래?}
    CheckSARLong -->|Yes| LongEntry[롱 포지션 진입]
    
    %% Short Position Flow
    CheckPrice -->|아래| CheckShort{MACD Line이\nSignal Line을\n하향돌파?}
    CheckShort -->|Yes| CheckSARShort{Parabolic SAR가\n캔들 위?}
    CheckSARShort -->|Yes| ShortEntry[숏 포지션 진입]
    
    %% Position Management
    LongEntry --> SetSL[스탑로스 설정:\n- Parabolic SAR 위치\n- 최대 0.7% 제한]
    ShortEntry --> SetSL
    SetSL --> SetTP[익절 설정:\n1:1 리스크 비율]
    SetTP --> Monitor[포지션 모니터링]
    
    %% Exit Conditions
    Monitor --> Exit{익절/손절\n도달?}
    Exit -->|Yes| ClosePosition[포지션 종료]
    Exit -->|No| Monitor
    
    %% Reset Flow
    ClosePosition --> Start

    %% Reject Cases
    CheckLong -->|No| Start
    CheckShort -->|No| Start
    CheckSARLong -->|No| Start
    CheckSARShort -->|No| Start
```


## 프로그램 아키텍처

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
