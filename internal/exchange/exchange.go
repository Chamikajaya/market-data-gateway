// All exchange adapters should satisfy this interface - open/closed principle

package exchange

import (
	"context"

	"market-gw.com/internal/domain"
)

type Exchange interface {

	// passing context -> allows main prog to send a cancel signal to the adapter - when user presses CTRL + C - shut down ws and exit cleanly
	// <- read only channel is returned. Adapter sends data to channel, pipeline is only able to read data from it
	Run(ctx context.Context) (<-chan domain.Update, error)
	Name() string
}
