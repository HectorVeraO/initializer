package parameters_test

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
)

// TestReplaceAndSetOverrideNeverLeaveAnOverrideOutsideTheConstraints races a
// Replace that lowers maxValue against a SetOverride above the new maximum.
// Whatever the order, exactly one of them loses and the override, if stored,
// is within the constraints that are stored.
func TestReplaceAndSetOverrideNeverLeaveAnOverrideOutsideTheConstraints(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	admin := globalPrincipal(t)
	tenant := principal(t, randomUUID(t))
	const rounds = 50
	for round := 0; round < rounds; round++ {
		key := fmt.Sprintf("race.round_%d", round)
		mustCreate(t, svc, admin, fmt.Sprintf(`{"key":%q,"valueType":"number","minValue":1,"maxValue":100,"defaultValue":1}`, key))

		var replaceErr, overrideErr error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, replaceErr = svc.Replace(ctx, admin, key, body(t, `{"minValue":1,"maxValue":10,"defaultValue":1}`))
		}()
		go func() {
			defer wg.Done()
			<-start
			_, overrideErr = svc.SetOverride(ctx, tenant, key, body(t, `{"value":50}`))
		}()
		close(start)
		wg.Wait()

		if (replaceErr == nil) == (overrideErr == nil) {
			t.Fatalf("round %d: replace=%v override=%v, want exactly one to fail", round, replaceErr, overrideErr)
		}
		page, err := svc.List(ctx, tenant, pageOf(100))
		if err != nil {
			t.Fatal(err)
		}
		for _, view := range page.Items {
			if view.Key != key || view.Source != "tenant" {
				continue
			}
			effective, parseErr := strconv.ParseFloat(string(view.EffectiveValue), 64)
			if parseErr != nil || effective > *view.MaxValue || effective < *view.MinValue {
				t.Fatalf("round %d: override %s outside [%v, %v]", round, view.EffectiveValue, *view.MinValue, *view.MaxValue)
			}
		}
	}
}
