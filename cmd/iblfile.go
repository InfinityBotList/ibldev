/*
Copyright © 2022 Infinity Bot List
*/
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/InfinityBotList/ibl/internal/iblfile_legacyenc"
	"github.com/go-andiamo/splitter"
	"github.com/infinitybotlist/iblfile"
	"github.com/infinitybotlist/iblfile/encryptors/aes256"
	"github.com/infinitybotlist/iblfile/encryptors/noencryption"
	"github.com/infinitybotlist/iblfile/encryptors/pem"
	"github.com/spf13/cobra"
)

var iblFileCmd = &cobra.Command{
	Use:   "file",
	Short: "IBL file information",
	Long:  "Retrieve information about an IBL file",
}

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Gets info about a ibl file",
	Long:  `Gets info about a ibl file`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		filename := args[0]

		f, err := os.Open(filename)

		if err != nil {
			fmt.Println("ERROR: Failed to open file:", err)
			os.Exit(1)
		}

		defer f.Close()

		deducedFile, err := iblfile.DeduceType(f, false)

		if err != nil {
			fmt.Println("ERROR: Failed to deduce file type:", err)
			os.Exit(1)
		}

		fmt.Println("Deduced file type:", deducedFile.Type.String())

		if deducedFile.Type == iblfile.DeducedTypeAutoEncryptedFile_FullFile {
			// We need to decrypt it
			block, err := iblfile.QuickBlockParser(f)

			if err != nil {
				fmt.Println("ERROR: Failed to parse block:", err)
				os.Exit(1)
			}

			var file *iblfile.AutoEncryptedFile_FullFile

			pemEnc := pem.PemEncryptedSource{}
			aes256Enc := aes256.AES256Source{}
			noencryptionEnc := noencryption.NoEncryptionSource{}
			if string(block.Encryptor) == pemEnc.ID() {
				privKeyFile := cmd.Flag("priv-key").Value.String()

				if privKeyFile == "" {
					fmt.Println("ERROR: You must specify a private key to decrypt the seed with!")
					os.Exit(1)
				}

				privKeyFileContents, err := os.ReadFile(privKeyFile)

				if err != nil {
					fmt.Println("ERROR: Failed to read private key file:", err)
					os.Exit(1)
				}

				pemEnc.PrivateKey = privKeyFileContents
				file, err = iblfile.OpenAutoEncryptedFile_FullFile(f, &pemEnc)

				if err != nil {
					fmt.Println("ERROR: Failed to open auto encrypted file:", err)
					os.Exit(1)
				}
			} else if string(block.Encryptor) == aes256Enc.ID() {
				encKey := cmd.Flag("enc-key").Value.String()

				if encKey == "" {
					fmt.Println("ERROR: You must specify an encryption key to decrypt the seed with!")
					os.Exit(1)
				}

				aes256Enc.EncryptionKey = encKey
				file, err = iblfile.OpenAutoEncryptedFile_FullFile(f, &aes256Enc)

				if err != nil {
					fmt.Println("ERROR: Failed to open auto encrypted file:", err)
					os.Exit(1)
				}
			} else if string(block.Encryptor) == noencryptionEnc.ID() {
				file, err = iblfile.OpenAutoEncryptedFile_FullFile(f, &noencryption.NoEncryptionSource{})

				if err != nil {
					fmt.Println("ERROR: Failed to open auto encrypted file:", err)
					os.Exit(1)
				}
			} else {
				fmt.Println("ERROR: Invalid encryptor:", string(block.Encryptor), ". Try using the `iblcli upgrade` command to upgrade the file")
				os.Exit(1)
			}

			sections, err := file.Sections()

			if err != nil {
				fmt.Println("ERROR: Failed to get sections:", err)
				os.Exit(1)
			}

			deducedFile.Sections = sections
		}

		fmt.Println("Deduced sections:", iblfile.MapKeys(deducedFile.Sections))
		fmt.Println("Deduction parse errors:", deducedFile.ParseErrors)

		meta, err := iblfile.LoadMetadata(deducedFile.Sections)

		if err != nil {
			fmt.Println("ERROR: Failed to load metadata:", err)
			os.Exit(1)
		}

		fmt.Println("\n== Metadata ==")
		fmt.Println("Protocol:", meta.Protocol)
		fmt.Println("File Version:", meta.FormatVersion)
		fmt.Println("Type:", meta.Type)
		fmt.Println("Created At:", meta.CreatedAt)

		if deducedFile.Type == iblfile.DeducedTypeAutoEncryptedFile_PerSection {
			fmt.Println("\n== Section Encryptors ==")
			for section := range deducedFile.Sections {
				// All sections are blocks, so just quickblockparse them
				block, err := iblfile.QuickBlockParser(bytes.NewReader(deducedFile.Sections[section].Bytes()))

				if err != nil {
					fmt.Println("ERROR: Failed to parse block '"+section+"' :", err)
					continue
				}

				fmt.Println(section + ": " + string(block.Encryptor))
			}
		}

		format, err := iblfile.GetFormat(meta.Type)

		if err != nil {
			fmt.Println("WARNING: Unknown/unregistered format:", meta.Type, "due to error: ", err)
			os.Exit(1)
		}

		if format.GetExtended != nil {
			extendedMeta, err := format.GetExtended(deducedFile.Sections, meta)

			if err != nil {
				fmt.Println("ERROR:", err)
				os.Exit(1)
			}

			fmt.Println("\n== Extended Info ==")

			for k, v := range extendedMeta {
				// If v is a struct marshal it into newline seperated key: value format
				fmt.Println(k+":", v)
			}
		}
	},
}

var iblFileUpgrade = &cobra.Command{
	Use:   "upgrade <input file> <output file>",
	Short: "Upgrade a file protocol version where possible",
	Long:  `Upgrade a file protocol version where possible. This does not upgrade format versions. To upgrade format version, use a more specific convert command provided by the format.`,
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		// Open input file
		inputFile, err := os.Open(args[0])

		if err != nil {
			fmt.Println("ERROR: Failed to open input file:", err)
			os.Exit(1)
		}

		defer inputFile.Close()

		deducedFile, err := iblfile.DeduceType(inputFile, false)

		if err != nil {
			fmt.Println("ERROR: Failed to deduce file type:", err)
			os.Exit(1)
		}

		if len(deducedFile.Sections) == 0 {
			fmt.Println("ERROR: No sections found in file")
			os.Exit(1)
		}

		fmt.Println("Deduced sections:", iblfile.MapKeys(deducedFile.Sections))
		fmt.Println("Deduction parse errors:", deducedFile.ParseErrors)

		metaBuf, ok := deducedFile.Sections["meta"]

		if !ok {
			fmt.Println("ERROR: No metadata section found")
			os.Exit(1)
		}

		type Meta struct {
			CreatedAt time.Time `json:"c"`
			Protocol  string    `json:"p"`

			// Format version
			//
			// This can be used to create breaking changes to a file type without changing the entire protocol
			FormatVersion string `json:"v,omitempty"`

			// Encryption data, if a section is encrypted
			// This is a map that maps each section to its encryption data
			EncryptionData map[string]*iblfile_legacyenc.PemEncryptionData `json:"e,omitempty"`

			// Extra metadata attributes
			ExtraMetadata map[string]string `json:"m,omitempty"`

			// Type of the file
			Type string `json:"t"`
		}

		var meta Meta

		metaBytes := metaBuf.Bytes()

		err = json.NewDecoder(bytes.NewBuffer(metaBytes)).Decode(&meta)

		if err != nil {
			fmt.Println("ERROR: Failed to decode metadata:", err)
			os.Exit(1)
		}

		// Apply some normalization
		switch meta.Protocol {
		// rev4 was a big upgrade as it added namespacing
		case "frostpaw-rev4-e1":
			fmt.Println("INFO: Namespacing types")
			var renamesMap = map[string]string{
				"backup": "db.backup",
				"seed":   "db.seed",
			}

			if newName, ok := renamesMap[meta.Type]; ok {
				meta.Type = newName
			}

			// Update the protocol to next version
			meta.Protocol = "frostpaw-rev5-e1"

			var bufNew = bytes.NewBuffer([]byte{})

			err = json.NewEncoder(bufNew).Encode(meta)

			if err != nil {
				fmt.Println("ERROR: Failed to encode metadata:", err)
				os.Exit(1)
			}

			deducedFile.Sections["meta"] = bufNew
		}

		// Legacy encryption removal
		if len(meta.EncryptionData) > 0 {
			fmt.Println("NOTE: Legacy encryption data detected, trying to remove it...")

			argSplitter, err := splitter.NewSplitter('=', splitter.DoubleQuotes, splitter.SingleQuotes)

			if err != nil {
				panic("error initializing arg tokenizer: " + err.Error())
			}

			var keyMap = make(map[string][]byte, len(meta.EncryptionData))

			// Go through all cmdline arguments for paths to keys
			for _, args := range args {
				argsSplit, err := argSplitter.Split(args)

				if err != nil {
					fmt.Println("WARNING: Splitting args failed: ", err.Error())
				}

				if len(argsSplit) == 2 && strings.HasPrefix(argsSplit[0], "pem:") {
					// Open key file
					keyFile, err := os.ReadFile(argsSplit[1])

					if err != nil {
						fmt.Println("ERROR: Failed to open key file:", err)
						os.Exit(1)
					}

					keyMap[strings.TrimPrefix(argsSplit[0], "pem:")] = keyFile
				}
			}

			for section, encData := range meta.EncryptionData {
				keyFile, ok := keyMap[section]

				if !ok {
					fmt.Println("ERROR: No key found for section:", section, "\nHINT: You can specify a key for this section with `pem:<section>=<path>`")
					os.Exit(1)
				}

				// Decrypt data
				newBuf, err := iblfile_legacyenc.DecryptData(
					deducedFile.Sections[section],
					encData,
					keyFile,
				)

				if err != nil {
					fmt.Println("ERROR: Failed to decrypt section:", section, err)
					os.Exit(1)
				}

				deducedFile.Sections[section] = newBuf
			}
		}

		// Write new metadata of rev7 (latest supported by upgrade command right now)
		newMeta := iblfile.Meta{
			CreatedAt:     meta.CreatedAt,
			Protocol:      "frostpaw-rev7",
			FormatVersion: meta.FormatVersion,
			Type:          meta.Type,
			ExtraMetadata: meta.ExtraMetadata,
		}

		var newMetaBuf = bytes.NewBuffer([]byte{})
		err = json.NewEncoder(newMetaBuf).Encode(newMeta)

		if err != nil {
			fmt.Println("ERROR: Failed to encode new metadata:", err)
			os.Exit(1)
		}

		deducedFile.Sections["meta"] = newMetaBuf

		// Write output as nonencyrpted auto encrypted file
		outputFile, err := os.Create(args[1])

		if err != nil {
			fmt.Println("ERROR: Failed to open output file:", err)
			os.Exit(1)
		}

		newFile := iblfile.NewAutoEncryptedFile_FullFile(noencryption.NoEncryptionSource{})

		for name, buf := range deducedFile.Sections {
			err = newFile.WriteSection(buf, name)

			if err != nil {
				fmt.Println("ERROR: Failed to write section:", err)
				os.Exit(1)
			}
		}

		err = newFile.WriteOutput(outputFile)

		if err != nil {
			fmt.Println("ERROR: Failed to write output file:", err)
			os.Exit(1)
		}
	},
}

func init() {
	infoCmd.PersistentFlags().Bool("show-pubkey", false, "Whether or not to show the public key for the encrypted data")

	iblFileCmd.AddCommand(infoCmd)
	iblFileCmd.AddCommand(iblFileUpgrade)
	rootCmd.AddCommand(iblFileCmd)
}
