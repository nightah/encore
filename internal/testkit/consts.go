package testkit

// The statements a Fault can fail.
const (
	// Read fails statements that select from the table.
	Read Op = "READ"
	// Insert fails rows inserted into the table.
	Insert Op = "INSERT"
	// Update fails rows updated in the table.
	Update Op = "UPDATE"
	// Delete fails rows deleted from the table.
	Delete Op = "DELETE"
)

// FaultMessage is in the error of every write a Fault fails.
const FaultMessage = "testkit: injected fault"

// ReadFaultMessage is in the error of every read a Fault fails: SQLite's own, since the read is refused by its authorizer.
const ReadFaultMessage = "authorization denied"

// faultPrefix names the triggers and the counter table Faults adds to a database.
const faultPrefix = "testkit_fault"
