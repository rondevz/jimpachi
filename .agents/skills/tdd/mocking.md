# When to Mock

Mock at **system boundaries** only:

- External APIs (payment, email, etc.)
- Databases (sometimes - prefer a test database)
- Time and randomness
- File systems and subprocesses (sometimes)

Don't mock:

- Your own packages
- Internal collaborators
- Anything you control

## Designing for Mockability

At system boundaries, define small interfaces at the consumer boundary and provide fakes in tests.

**1. Use dependency injection**

Pass external dependencies in rather than constructing them inside behavior:

```go
// Easy to fake: the consumer declares the capability it needs.
type PaymentProcessor interface {
	Charge(context.Context, Money) error
}

func ProcessPaymentWith(ctx context.Context, order Order, processor PaymentProcessor) error {
	return processor.Charge(ctx, order.Total)
}

// Hard to fake: the system boundary is constructed inside behavior.
func ProcessPayment(ctx context.Context, order Order) error {
	client := stripe.NewClient(os.Getenv("STRIPE_KEY"))
	return client.Charge(ctx, order.Total)
}
```

**2. Prefer operation-specific interfaces over generic transport interfaces**

Create methods for the operations the consumer needs instead of making tests branch on generic requests:

```go
// GOOD: Each method has a specific input and output shape.
type UserOrders interface {
	User(context.Context, UserID) (User, error)
	Orders(context.Context, UserID) ([]Order, error)
	CreateOrder(context.Context, NewOrder) (Order, error)
}

// BAD: Every fake must branch on request details.
type APIClient interface {
	Do(context.Context, *http.Request) (*http.Response, error)
}
```

The operation-specific approach means:

- Each fake returns one specific shape
- No conditional logic in test setup
- Easier to see which external behavior a test exercises
- Compile-time checking per operation
