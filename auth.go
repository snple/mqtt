package mqtt

// Auth is an interface for authentication controllers.
type Auth interface {

	// Auth authenticates a user on CONNECT and returns true if a user is
	// allowed to join the server.
	Auth(client *Client) bool

	// ACL returns true if a user has read or write access to a given topic.
	ACL(client *Client, topic string, write bool) bool
}

// Allow is an auth controller which allows access to all connections and topics.
type AuthAllow struct{}

// Auth returns true if a username and password are acceptable. Allow always
// returns true.
func (a *AuthAllow) Auth(client *Client) bool {
	return true
}

// ACL returns true if a user has access permissions to read or write on a topic.
// Allow always returns true.
func (a *AuthAllow) ACL(client *Client, topic string, write bool) bool {
	return true
}

// Disallow is an auth controller which disallows access to all connections and topics.
type AuthDisallow struct{}

// Auth returns true if a username and password are acceptable. Disallow always
// returns false.
func (d *AuthDisallow) Auth(client *Client) bool {
	return false
}

// ACL returns true if a user has access permissions to read or write on a topic.
// Disallow always returns false.
func (d *AuthDisallow) ACL(client *Client, topic string, write bool) bool {
	return false
}
