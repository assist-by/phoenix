phoenix/
├── cmd/
│   └── trader/
│       └── main.go              # 메인 진입점, 어플리케이션 구동
│
├── internal/
│   ├── config/                  # 설정 관련
│   │   └── config.go           # 환경변수 설정 관리
│   │
│   ├── market/                  # 시장 데이터 관련
│   │   ├── client.go           # 바이낸스 API 클라이언트
│   │   ├── collector.go        # 데이터 수집기
│   │   └── types.go            # 마켓 관련 타입 정의
│   │
│   ├── notification/            # 알림 관련
│   │   ├── types.go            # 알림 타입 정의
│   │   └── discord/            # 디스코드 알림 구현
│   │       ├── client.go       # 디스코드 클라이언트
│   │       ├── embed.go        # 디스코드 임베드 메시지
│   │       └── webhook.go      # 웹훅 처리
│   │
│   ├── analysis/               # 분석 관련
│   │   ├── indicator/         # 기술적 지표
│   │   │   ├── types.go      # 지표 타입 정의
│   │   │   ├── ema.go        # EMA 지표
│   │   │   ├── macd.go       # MACD 지표
│   │   │   └── sar.go        # Parabolic SAR 지표
│   │   └── signal/           # 시그널 관련
│   │       ├── detector.go   # 시그널 감지기
│   │       └── types.go      # 시그널 타입 정의
│   │
│   └── scheduler/             # 스케줄러
│       └── scheduler.go       # 작업 스케줄링
│
├── .env                       # 환경변수
├── .env.example              # 환경변수 예시
├── .gitignore
├── go.mod
└── README.md