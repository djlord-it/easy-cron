package cron

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

type Parser struct {
	parser cron.Parser
}

func NewParser() *Parser {
	return &Parser{
		parser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

func (p *Parser) Parse(expression string, timezone string) (Schedule, error) {
	sched, err := p.parser.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("parse cron: %w", err)
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}

	return &schedule{sched: sched, loc: loc}, nil
}

type Schedule interface {
	Next(after time.Time) time.Time
}

type schedule struct {
	sched cron.Schedule
	loc   *time.Location
}

func (s *schedule) Next(after time.Time) time.Time {
	return s.sched.Next(after.In(s.loc))
}
