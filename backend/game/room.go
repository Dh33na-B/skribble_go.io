package game


type Room struct{
	ID string 
	HostID string 
	Players map[string]bool
	IsStarted bool 
}