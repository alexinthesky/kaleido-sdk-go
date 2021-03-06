// Copyright 2018 Kaleido, a ConsenSys business

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package registry

import (
	"errors"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/kaleido-io/kaleido-sdk-go/common"
	"github.com/kaleido-io/kaleido-sdk-go/kaleido/registry"
	"github.com/spf13/cobra"
)

var groupsListCmd = &cobra.Command{
	Use:   "groups",
	Short: "List the groups within an org",
	Args: func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}

		// arg[0] is the name and must start with a 0x or a /
		name := args[0]
		if name[:2] != "0x" && name[:1] != "/" {
			return errors.New("name of the group parent must begin with a 0x or must be specified as a path beginning with /")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		group := registry.Group{}
		var verifiedOrgs *[]registry.ContractOrganization
		var err error
		nodeID := registry.Utils().GenerateNodeID(args[0])
		var node [32]byte
		nodeBytes, _ := hexutil.Decode(nodeID)
		copy(node[:], nodeBytes)

		if verifiedOrgs, err = group.InvokeList(node); err != nil {
			cmd.SilenceUsage = true  // not a usage error at this point
			cmd.SilenceErrors = true // no need to display Error:, this still displays the error that is returned from RunE
			return err
		}
		common.PrintJSON(*verifiedOrgs)
		return nil
	},
}

var groupGetCmd = &cobra.Command{
	Use:   "group",
	Short: "Get the group details identified by a path",
	Args: func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}

		// arg[0] is the name and must start with a 0x or a /
		name := args[0]
		if name[:2] != "0x" && name[:1] != "/" {
			return errors.New("name of a group must begin with a 0x or must be specified as a path beginning with /")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		group := &registry.Group{
			Name: args[0],
		}
		var err error
		if err = group.InvokeGet(); err != nil {
			cmd.SilenceUsage = true  // not a usage error at this point
			cmd.SilenceErrors = true // no need to display Error:, this still displays the error that is returned from RunE
			return err
		}
		return nil
	},
}

var groupCreateCmd = &cobra.Command{
	Use:   "group",
	Short: "Create a group at the given path (for an org)",
	Args: func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		parent := cmd.Flags().Lookup("parent").Value.String()
		if parent[:2] != "0x" && parent[:1] != "/" {
			return errors.New("flag 'parent' value must start with either a '0x' or a '/'")
		}

		var group *registry.Group
		group = &registry.Group{
			Owner:  cmd.Flags().Lookup("owner").Value.String(),
			Name:   args[0],
			Parent: parent,
		}

		var keystorePath string
		var signer string

		keystorePath = cmd.Flags().Lookup("keystore").Value.String()
		signer = cmd.Flags().Lookup("signer").Value.String()

		var err error
		if err = group.InvokeCreate(keystorePath, signer); err != nil {
			cmd.SilenceUsage = true  // not a usage error at this point
			cmd.SilenceErrors = true // no need to display Error:, this still displays the error that is returned from RunE
			return err
		}
		return nil
	},
}

func init() {
	initCreateGroupCmd()

	createCmd.AddCommand(groupCreateCmd)
	getCmd.AddCommand(groupGetCmd)
	getCmd.AddCommand(groupsListCmd)
}

func initCreateGroupCmd() {
	flags := groupCreateCmd.Flags()

	flags.StringP("parent", "p", "", "Name path to the parent org or group")
	flags.VarP(&common.EthereumAddress{}, "signer", "s", "Account owned by parent (used to sign tx)")
	flags.StringP("keystore", "k", "", "Keystore directory path so account can be used to sign tx")
	flags.VarP(&common.EthereumAddress{}, "owner", "o", "Ethereum account that will be the admin of the group")

	groupCreateCmd.MarkFlagRequired("parent")
	groupCreateCmd.MarkFlagRequired("signer")
	groupCreateCmd.MarkFlagRequired("keystore")
	groupCreateCmd.MarkFlagRequired("owner")
}
