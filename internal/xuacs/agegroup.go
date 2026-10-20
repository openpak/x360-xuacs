package xuacs

import (
	"context"
	"encoding/binary"
	"errors"
	"time"
)

// PathAgeGroup is native service 5 opcode 0x63C.
const PathAgeGroup = "/xuacs/XeUserGetAgeGroup.ashx"

type birthdayCore interface {
	Birthday(context.Context, string) (string, error)
}

func (c CoreClient) Birthday(ctx context.Context, account string) (string, error) {
	return c.Client.Birthday(ctx, account)
}

func (h *Handler) ageGroup(ctx context.Context, body []byte, usersHeader string) ([]byte, error) {
	if len(body) != 20 {
		return nil, bad("0x63C: expected 20 bytes")
	}
	users, err := ticketUsers(usersHeader)
	if err != nil {
		return nil, err
	}
	account := users[binary.LittleEndian.Uint64(body)]
	if account == "" {
		return nil, deny(403, "age-group user is not on tunnel ticket")
	}
	core, ok := h.Core.(birthdayCore)
	if !ok {
		return nil, deny(503, "account policy unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	birthday, err := core.Birthday(ctx, account)
	if err != nil {
		return nil, deny(503, "account policy unavailable")
	}
	group, err := ageGroupForBirthday(birthday, h.now())
	if err != nil {
		return nil, deny(503, "account policy unavailable")
	}
	return binary.LittleEndian.AppendUint32(nil, group), nil
}

func ageGroupForBirthday(birthday string, now time.Time) (uint32, error) {
	// Uncollected birthdays retain the restrictive child policy.
	if birthday == "" {
		return 0, nil
	}
	born, err := time.Parse("2006-01-02", birthday)
	if err != nil {
		return 0, err
	}
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if born.After(today) {
		return 0, errors.New("future birthday")
	}
	age := now.Year() - born.Year()
	if now.Month() < born.Month() || (now.Month() == born.Month() && now.Day() < born.Day()) {
		age--
	}
	if age < 13 {
		return 0, nil
	}
	if age < 18 {
		return 1, nil
	}
	return 2, nil
}
