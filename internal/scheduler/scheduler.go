package scheduler

import (
	"context"
	"log"
	"time"
)

// Task는 스케줄러가 실행할 작업을 정의하는 인터페이스입니다
type Task interface {
	Execute(ctx context.Context) error
}

// Scheduler는 정해진 시간에 작업을 실행하는 스케줄러입니다
type Scheduler struct {
	interval time.Duration
	task     Task
	stopCh   chan struct{}
}

// NewScheduler는 새로운 스케줄러를 생성합니다
func NewScheduler(interval time.Duration, task Task) *Scheduler {
	return &Scheduler{
		interval: interval,
		task:     task,
		stopCh:   make(chan struct{}),
	}
}

// Start는 스케줄러를 시작합니다
// internal/scheduler/scheduler.go

func (s *Scheduler) Start(ctx context.Context) error {
	// 다음 실행 시간 계산
	now := time.Now()
	nextRun := now.Truncate(s.interval).Add(s.interval)
	waitDuration := nextRun.Sub(now)

	log.Printf("다음 실행까지 %v 대기 (다음 실행: %s)",
		waitDuration.Round(time.Second),
		nextRun.Format("15:04:05"))

	timer := time.NewTimer(waitDuration)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-s.stopCh:
			return nil

		case <-timer.C:
			// 작업 실행
			if err := s.task.Execute(ctx); err != nil {
				log.Printf("작업 실행 실패: %v", err)
				// 에러가 발생해도 계속 실행
			}

			// 다음 실행 시간 계산
			now := time.Now()
			nextRun = now.Truncate(s.interval).Add(s.interval)
			waitDuration = nextRun.Sub(now)

			log.Printf("다음 실행까지 %v 대기 (다음 실행: %s)",
				waitDuration.Round(time.Second),
				nextRun.Format("15:04:05"))

			// 타이머 리셋
			timer.Reset(waitDuration)
		}
	}
}

// Stop은 스케줄러를 중지합니다
func (s *Scheduler) Stop() {
	close(s.stopCh)
}
