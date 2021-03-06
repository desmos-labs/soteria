package export

import (
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	coretypes "github.com/tendermint/tendermint/rpc/core/types"

	"github.com/cosmos/cosmos-sdk/simapp/params"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting/exported"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/forbole/juno/v2/node"
	nodebuilder "github.com/forbole/juno/v2/node/builder"
	nodeconfig "github.com/forbole/juno/v2/node/config"
	juno "github.com/forbole/juno/v2/types"

	"github.com/desmos-labs/soteria/types"
)

// Exporter allows to easily export the various accounts data
type Exporter struct {
	node node.Node

	limitHeight int64
}

// NewExporter returns a new Exporter instance
func NewExporter(cfg nodeconfig.Config, config *params.EncodingConfig) (*Exporter, error) {
	// Build the node
	chainNode, err := nodebuilder.BuildNode(cfg, config)
	if err != nil {
		return nil, err
	}

	return &Exporter{
		node: chainNode,
	}, nil
}

// SetLimitHeight sets the given height as the height limit to query the transactions for
func (e *Exporter) SetLimitHeight(height int64) error {
	if height < 0 {
		return fmt.Errorf("invalid height: %d", height)
	}
	e.limitHeight = height
	return nil
}

// getLimitHeight returns the max height to be used when searching for transactions
func (e *Exporter) getLimitHeight() (int64, error) {
	if e.limitHeight != 0 {
		return e.limitHeight, nil
	}

	return e.node.LatestHeight()
}

// FixVestingAccount fixes the given account by properly tracking all delegations and
// undelegations that it has ever performed
func (e *Exporter) FixVestingAccount(account exported.VestingAccount) error {
	accountAddress := account.GetAddress().String()

	height, err := e.getLimitHeight()
	if err != nil {
		return err
	}

	var txs []*coretypes.ResultTx

	// Get the delegation txs
	delegateTxs, err := e.getTransactions("delegate", accountAddress, height)
	if err != nil {
		return err
	}
	txs = append(txs, delegateTxs...)

	delegateTxs, err = e.getTransactions(sdk.MsgTypeURL(&stakingtypes.MsgDelegate{}), accountAddress, height)
	if err != nil {
		return err
	}
	txs = append(txs, delegateTxs...)

	// Get the unbonding txs
	undelegateTxs, err := e.getTransactions("begin_unbonding", accountAddress, height)
	if err != nil {
		return err
	}
	txs = append(txs, undelegateTxs...)

	undelegateTxs, err = e.getTransactions(sdk.MsgTypeURL(&stakingtypes.MsgUndelegate{}), accountAddress, height)
	if err != nil {
		return err
	}
	txs = append(txs, undelegateTxs...)

	// Order the txs by ascending height
	sort.Slice(txs, func(i, j int) bool {
		return txs[i].Height < txs[j].Height
	})

	for _, tx := range txs {
		// Get the tx details
		junoTx, err := e.node.Tx(hex.EncodeToString(tx.Tx.Hash()))
		if err != nil {
			return err
		}

		for _, msg := range junoTx.GetMsgs() {
			switch sdkMsg := msg.(type) {
			case *stakingtypes.MsgDelegate:
				err = e.handleMsgDelegate(account, junoTx, sdkMsg)
				if err != nil {
					return err
				}

			case *stakingtypes.MsgUndelegate:
				e.handleMsgUndelegate(account, sdkMsg)
			}
		}
	}

	return nil
}

// getTransactions returns all the transactions searching for messages with the given action made
// by the provided address at limited by the given height
func (e *Exporter) getTransactions(action, address string, height int64) ([]*coretypes.ResultTx, error) {
	messageQuery := "message.action = '%[1]s' AND message.sender = '%[2]s' AND tx.height <= %[3]d"
	query := fmt.Sprintf(messageQuery, action, address, height)
	return types.QueryTxs(e.node, query)
}

// handleMsgDelegate handles the given MsgDelegate present inside the provided
// transaction and send by the given account
func (e *Exporter) handleMsgDelegate(account exported.VestingAccount, tx *juno.Tx, msg *stakingtypes.MsgDelegate) error {
	// Get the timestamp
	timestamp, err := time.Parse(time.RFC3339, tx.Timestamp)
	if err != nil {
		return err
	}

	// Track the delegation
	balance := sdk.NewCoins(sdk.NewCoin(msg.Amount.Denom, msg.Amount.Amount.AddRaw(10000)))
	account.TrackDelegation(timestamp, balance, sdk.NewCoins(msg.Amount))

	return nil
}

// handleMsgUndelegate handles the given MsgUndelegate present inside the provided
// transaction and send by the given account
func (e *Exporter) handleMsgUndelegate(account exported.VestingAccount, msg *stakingtypes.MsgUndelegate) {
	// Track the undelegation
	account.TrackUndelegation(sdk.NewCoins(msg.Amount))
}
