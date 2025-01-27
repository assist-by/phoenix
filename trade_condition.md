롱(Long) 포지션 진입 조건:

가격이 EMA200보다 높음 (price > EMA200)
Parabolic SAR가 현재 봉의 저가보다 낮음 (SAR < Low)
MACD가 시그널라인을 상향돌파 (MACD가 Signal Line을 아래에서 위로 크로스)

숏(Short) 포지션 진입 조건:

가격이 EMA200보다 낮음 (price < EMA200)
Parabolic SAR가 현재 봉의 고가보다 높음 (SAR > High)
MACD가 시그널라인을 하향돌파 (MACD가 Signal Line을 위에서 아래로 크로스)


```mermaid
flowchart TD
    Start([시작]) --> Init[초기 설정:]
    Init --> CheckPrice{가격이 200 EMA 기준 위/아래?}
    
    %% Long Position Flow
    CheckPrice -->|위| CheckLongMACD{MACD Line이 Signal Line을 상향돌파?}
    CheckLongMACD -->|Yes| CheckLongHist{히스토그램 절대값 >= MinHist?}
    CheckLongHist -->|Yes| CheckSARLong{Parabolic SAR가 캔들 아래?}
    CheckSARLong -->|Yes| LongEntry[롱 포지션 진입]
    
    %% Short Position Flow
    CheckPrice -->|아래| CheckShortMACD{MACD Line이 Signal Line을 하향돌파?}
    CheckShortMACD -->|Yes| CheckShortHist{히스토그램 절대값 >= MinHist?}
    CheckShortHist -->|Yes| CheckSARShort{Parabolic SAR가 캔들 위?}
    CheckSARShort -->|Yes| ShortEntry[숏 포지션 진입]
    
    %% Position Management
    LongEntry --> SetSL[스탑로스 설정]
    ShortEntry --> SetSL
    SetSL --> SetTP[익절 설정]
    SetTP --> Monitor[포지션 모니터링]
    
    %% Exit Conditions
    Monitor --> Exit{익절/손절 도달?}
    Exit -->|Yes| ClosePosition[포지션 종료]
    Exit -->|No| Monitor
    
    %% Reset Flow
    ClosePosition --> Start

    %% Reject Cases
    CheckLongMACD -->|No| Start
    CheckShortMACD -->|No| Start
    CheckLongHist -->|No| Start
    CheckShortHist -->|No| Start
    CheckSARLong -->|No| Start
    CheckSARShort -->|No| Start

    %% Entry Notes
    subgraph Entry[진입 조건]
        EntryLong[롱 진입 조건]
        EntrySht[숏 진입 조건]
    end

    %% Risk Notes
    subgraph Risk[리스크 관리]
        RiskSL[스탑로스: SAR 기준]
        RiskTP[익절: 1:1 리스크 비율]
    end
```