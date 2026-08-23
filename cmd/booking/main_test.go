package main

import (
	"testing"
	"time"
)

func TestParseRestoreWindow(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want time.Duration
		err  bool
	}{
		{name: "пустая строка — дефолт 10m", raw: "", want: 10 * time.Minute},
		{name: "секунды", raw: "90s", want: 90 * time.Second},
		{name: "часы", raw: "1h", want: time.Hour},
		{name: "невалидное значение", raw: "abc", err: true},
		{name: "число без единицы", raw: "10", err: true},
		{name: "неположительное значение", raw: "-5m", err: true},
		{name: "ноль", raw: "0s", err: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRestoreWindow(tt.raw)
			if tt.err {
				if err == nil {
					t.Fatalf("ожидали ошибку, получили %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("непредвиденная ошибка: %v", err)
			}
			if got != tt.want {
				t.Fatalf("окно = %v, хотим %v", got, tt.want)
			}
		})
	}
}
