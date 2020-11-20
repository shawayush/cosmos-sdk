package keeper_test

import (
	"encoding/json"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/ibc/applications/transfer/types"
	clienttypes "github.com/cosmos/cosmos-sdk/x/ibc/core/02-client/types"
	channeltypes "github.com/cosmos/cosmos-sdk/x/ibc/core/04-channel/types"
	ibctesting "github.com/cosmos/cosmos-sdk/x/ibc/testing"
	"github.com/tendermint/tendermint/crypto"
	"io/ioutil"
	"strconv"
	"strings"
)


type TlaBalance struct {
	Address []string  `json:"address"`
	Denom []string    `json:"denom"`
	Amount int64      `json:"amount"`
}

type TlaFungibleTokenPacketData struct {
	Sender string     `json:"sender"`
	Receiver string   `json:"receiver"`
	Amount int        `json:"amount"`
	Denom []string    `json:"denom"`
}

type TlaFungibleTokenPacket struct {
	SourceChannel string `json:"sourceChannel"`
	SourcePort string    `json:"sourcePort"`
	DestChannel string   `json:"destChannel"`
	DestPort string      `json:"destPort"`
	Data TlaFungibleTokenPacketData `json:"data"`
}

type TlaOnRecvPacketTestCase = struct {
	// The required subset of bank balances
	BankBefore []TlaBalance        `json:"bankBefore"`
	// The packet to process
	Packet TlaFungibleTokenPacket  `json:"packet"`
	// The handler to call
	Handler string                 `json:"handler"`
	// The expected changes in the bank
	BankAfter []TlaBalance        `json:"bankAfter"`
	// Whether OnRecvPacket should fail or not
	Error bool                     `json:"error"`
}

type FungibleTokenPacket struct {
	SourceChannel string
	SourcePort string
	DestChannel string
	DestPort string
	Data types.FungibleTokenPacketData
}

type OnRecvPacketTestCase = struct {
	description string
	// The required subset of bank balances
	bankBefore []Balance
	// The packet to process
	packet FungibleTokenPacket
	// The handler to call
	handler string
	// The expected bank state after processing (wrt. bankBefore)
	bankAfter []Balance
	// Whether OnRecvPacket should pass or fail
	pass bool
}

type OwnedCoin struct {
	Address string
	Denom string
}

type Balance struct {
	Address string
	Denom string
	Amount sdk.Int
}


func AddressFromString(address string) string {
	return sdk.AccAddress(crypto.AddressHash([]byte(address))).String()
}

func AddressFromTla(addr []string) string {
	if len(addr) != 3 {
		panic("failed to convert from TLA+ address: wrong number of address components")
	}
	s := ""
	if len(addr[0]) == 0 && len(addr[1]) == 0 {
		// simple address: id
		s = addr[2]
	} else if len(addr[2]) == 0   {
		// escrow address: port + channel
		s = addr[0] + addr[1]
	} else {
		panic("failed to convert from TLA+ address: neither simple nor escrow address")
	}
	return AddressFromString(s)
}

func DenomFromTla(denom []string) string {
	if len(denom) != 3 {
		panic("failed to convert from TLA+ denom")
	}
	s := ""
	if len(denom[0]) == 0 && len(denom[1]) == 0 {
		// native denomination
		s = denom[2]
	} else  {
		s = strings.Join(denom, "/")
	}
	return s
}

func BalanceFromTla(balance TlaBalance) Balance {
	return Balance{
		Address: AddressFromTla(balance.Address),
		Denom:   DenomFromTla(balance.Denom),
		Amount:  sdk.NewInt(balance.Amount),
	}
}

func BalancesFromTla(tla []TlaBalance) []Balance {
	balances := make([]Balance,0)
	for _, b := range tla {
		balances = append(balances, BalanceFromTla(b))
	}
	return balances
}

func FungibleTokenPacketFromTla(packet TlaFungibleTokenPacket) FungibleTokenPacket {
	return FungibleTokenPacket{
		SourceChannel: packet.SourceChannel,
		SourcePort:    packet.SourcePort,
		DestChannel:   packet.DestChannel,
		DestPort:      packet.DestPort,
		Data:          types.NewFungibleTokenPacketData(
			DenomFromTla(packet.Data.Denom),
			uint64(packet.Data.Amount),
			AddressFromString(packet.Data.Sender),
			AddressFromString(packet.Data.Receiver)),
	}
}

func OnRecvPacketTestCaseFromTla(tc TlaOnRecvPacketTestCase) OnRecvPacketTestCase {
	return OnRecvPacketTestCase{
		description: "auto-generated",
		bankBefore:  BalancesFromTla(tc.BankBefore),
		packet:      FungibleTokenPacketFromTla(tc.Packet),
		handler: tc.Handler,
		bankAfter:  BalancesFromTla(tc.BankAfter), // TODO different semantics
		pass:        !tc.Error,
	}
}

type Bank struct {
	balances map[OwnedCoin]sdk.Int
}

// Make an empty bank
func MakeBank() Bank {
	return Bank{ balances: make(map[OwnedCoin]sdk.Int) }
}

// Subtract other bank from this bank
func (bank *Bank) Sub(other *Bank) Bank {
	diff := MakeBank()
	for coin, amount := range bank.balances {
		otherAmount, exists := other.balances[coin]
		if exists {
			diff.balances[coin] = amount.Sub(otherAmount)
		} else {
			diff.balances[coin] = amount
		}
	}
	for coin, amount := range other.balances {
		if _, exists := bank.balances[coin]; !exists {
			diff.balances[coin] = amount.Neg()
		}
	}
	return diff
}

// Set specific bank balance
func (bank *Bank) SetBalance(address string, denom string, amount sdk.Int) {
	bank.balances[OwnedCoin{address, denom}] = amount
}

// Set several balances at once
func (bank *Bank) SetBalances(balances []Balance) {
	for _, balance := range balances {
		bank.balances[OwnedCoin{balance.Address, balance.Denom}] = balance.Amount
	}
}

func NullCoin() OwnedCoin {
	return OwnedCoin{
		Address: AddressFromString(""),
		Denom:   "",
	}
}

// Set several balances at once
func BankFromBalances(balances []Balance) Bank{
	bank := MakeBank()
	for _, balance := range balances {
		coin := OwnedCoin{balance.Address, balance.Denom}
		if coin != NullCoin() { // ignore null coin
			bank.balances[coin] = balance.Amount
		}
	}
	return bank
}


// String representation of all bank balances
func (bank *Bank) String() string {
	str := ""
	for coin, amount := range bank.balances {
		str += coin.Address + " : " + coin.Denom + " = " + amount.String() + "\n"
	}
	return str
}

// String representation of non-zero bank balances
func (bank *Bank) NonZeroString() string {
	str := ""
	for coin, amount := range bank.balances {
		if !amount.IsZero() {
			str += coin.Address + " : " + coin.Denom + " = " + amount.String() + "\n"
		}
	}
	return str
}

// Construct a bank out of the chain bank
func BankOfChain(chain *ibctesting.TestChain) Bank {
	bank := MakeBank()
	chain.App.BankKeeper.IterateAllBalances(chain.GetContext(), func(address sdk.AccAddress, coin sdk.Coin) (stop bool){
		fullDenom := coin.Denom
		if strings.HasPrefix(coin.Denom, "ibc/") {
			fullDenom, _ = chain.App.TransferKeeper.DenomPathFromHash(chain.GetContext(), coin.Denom)
		}
		bank.SetBalance(address.String(), fullDenom, coin.Amount)
		return false
	})
	return bank
}

// Set balances of the chain bank for balances present in the bank
func (suite *KeeperTestSuite) SetChainBankBalances(chain *ibctesting.TestChain, bank *Bank) error {
	for coin, amount := range bank.balances {
		address, err := sdk.AccAddressFromBech32(coin.Address)
		if err != nil {
			return err
		}
		trace := types.ParseDenomTrace(coin.Denom)
		err = chain.App.BankKeeper.SetBalance(chain.GetContext(), address, sdk.NewCoin(trace.IBCDenom(), amount))
		if err != nil {
			return err
		}
	}
	return nil
}

// Check that the state of the bank is the bankBefore + expectedBankChange
func (suite *KeeperTestSuite) CheckBankBalances(chain *ibctesting.TestChain, bankBefore *Bank, expectedBankChange *Bank) error {
	bankAfter := BankOfChain(chain)
	bankChange := bankAfter.Sub(bankBefore)
	diff := bankChange.Sub(expectedBankChange)
	NonZeroString := diff.NonZeroString()
	if len(NonZeroString) != 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "Unexpected changes in the bank: \n" + NonZeroString)
	}
	return nil
}

func (suite *KeeperTestSuite) TestModelBasedOnRecvPacket() {
	var tlaTestCases = []TlaOnRecvPacketTestCase{}

	filename := "recv-test.json"
	jsonBlob, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(fmt.Errorf("Failed to read JSON test fixture: %w", err))
	}
	err = json.Unmarshal([]byte(jsonBlob), &tlaTestCases)
	if err != nil {
		panic(fmt.Errorf("Failed to parse JSON test fixture: %w", err))
	}

	suite.SetupTest()
	for i, tlaTc := range tlaTestCases {
		tc := OnRecvPacketTestCaseFromTla(tlaTc)
		description := filename + " # " + strconv.Itoa(i+1)
		suite.Run(fmt.Sprintf("Case %s", description), func() {
			seq := uint64(1)
			packet := channeltypes.NewPacket(tc.packet.Data.GetBytes(), seq, tc.packet.SourcePort, tc.packet.SourceChannel, tc.packet.DestPort, tc.packet.DestChannel, clienttypes.NewHeight(0, 100), 0)
			bankBefore := BankFromBalances(tc.bankBefore)
			realBankBefore := BankOfChain(suite.chainB)
			// First validate the packet itself (mimics what happens when the packet is being sent and/or received)
			err := packet.ValidateBasic()
			if err != nil {
				suite.Require().False(tc.pass, err.Error())
				return
			}
			switch tc.handler {
				case "OnRecvPacket": err = suite.chainB.App.TransferKeeper.OnRecvPacket(suite.chainB.GetContext(), packet, tc.packet.Data)
				case "OnTimeoutPacket": err = suite.chainB.App.TransferKeeper.OnTimeoutPacket(suite.chainB.GetContext(), packet, tc.packet.Data)
				default: err = fmt.Errorf("Unknown handler:  %s", tc.handler)
			}
			if err != nil {
				suite.Require().False(tc.pass, err.Error())
				return
			}
			bankAfter := BankFromBalances(tc.bankAfter)
			expectedBankChange := bankAfter.Sub(&bankBefore)
			if err := suite.CheckBankBalances(suite.chainB, &realBankBefore, &expectedBankChange); err != nil {
				suite.Require().False(tc.pass, err.Error())
				return
			}
			suite.Require().True(tc.pass)
		})
	}
}