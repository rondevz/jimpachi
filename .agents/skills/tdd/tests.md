# Good and Bad Tests

## Good Tests

**Integration-style**: Test through real interfaces, not mocks of internal parts.

```go
func TestCheckoutConfirmsValidCart(t *testing.T) {
	cart := NewCart()
	cart.Add(product)

	result, err := Checkout(cart, paymentMethod)
	if err != nil {
		t.Fatalf("Checkout() error = %v", err)
	}
	if result.Status != Confirmed {
		t.Errorf("Checkout() status = %q, want %q", result.Status, Confirmed)
	}
}
```

Characteristics:

- Tests behavior users/callers care about
- Uses public API only
- Survives internal refactors
- Describes WHAT, not HOW
- Makes assertions only needed to establish the behavior

## Bad Tests

**Implementation-detail tests**: Coupled to internal structure.

```go
func TestCheckoutCallsPaymentProcessor(t *testing.T) {
	processor := &fakeProcessor{}
	_, _ = Checkout(cart, processor)

	if processor.calls != 1 {
		t.Fatalf("processor calls = %d, want 1", processor.calls)
	}
}
```

Red flags:

- Mocking internal collaborators
- Testing unexported behavior directly
- Asserting on call counts/order when the caller cannot observe them
- Test breaks when refactoring without behavior change
- Test name describes HOW not WHAT
- Verifying through external means instead of interface

```go
// BAD: Bypasses the public interface to query persistence.
func TestCreateUserSavesToDatabase(t *testing.T) {
	_, _ = service.CreateUser(ctx, "Alice")
	if !rowExists(t, db, "Alice") {
		t.Fatal("user row was not found")
	}
}

// GOOD: Verifies through the public interface.
func TestCreateUserMakesUserRetrievable(t *testing.T) {
	user, err := service.CreateUser(ctx, "Alice")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	retrieved, err := service.User(ctx, user.ID)
	if err != nil {
		t.Fatalf("User() error = %v", err)
	}
	if retrieved.Name != "Alice" {
		t.Errorf("User() name = %q, want %q", retrieved.Name, "Alice")
	}
}
```

**Tautological tests**: Expected value restates the implementation, so the test passes by construction.

```go
// BAD: The expected value repeats the implementation's calculation.
func TestCalculateTotalSumsLineItems_Tautological(t *testing.T) {
	items := []LineItem{{Price: 10}, {Price: 5}}
	var want int
	for _, item := range items {
		want += item.Price
	}
	if got := CalculateTotal(items); got != want {
		t.Errorf("CalculateTotal() = %d, want %d", got, want)
	}
}

// GOOD: The expected value is an independent, known literal.
func TestCalculateTotalSumsLineItems_KnownLiteral(t *testing.T) {
	if got := CalculateTotal([]LineItem{{Price: 10}, {Price: 5}}); got != 15 {
		t.Errorf("CalculateTotal() = %d, want 15", got)
	}
}
```
