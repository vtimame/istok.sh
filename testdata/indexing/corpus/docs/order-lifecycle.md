# Order lifecycle

The HTTP order endpoint delegates reads and writes to the order service. The
service checks the repository before charging the payment gateway so repeated
order identifiers can be handled idempotently.

Repository `ErrNotFound` is a domain result. HTTP adapters may translate it to
status 404, while infrastructure failures remain status 500.
