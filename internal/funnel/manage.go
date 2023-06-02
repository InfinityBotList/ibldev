package funnel

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/InfinityBotList/ibl/helpers"
	"github.com/InfinityBotList/ibl/types"
	"github.com/infinitybotlist/eureka/crypto"
)

type FunnelCommand = func(*types.FunnelList) error

type funnelAction struct {
	Name   string
	Action FunnelCommand
}

var funnelActions = map[string]funnelAction{
	"P": {
		Name:   "Change webserver port",
		Action: portMan,
	},
	"D": {
		Name:   "Set domain",
		Action: setDomain,
	},
	"N": {
		Name:   "New funnel",
		Action: newFunnel,
	},
	"Q": {
		Name: "Save And Quit",
		Action: func(funnels *types.FunnelList) error {
			err := helpers.WriteConfig("funnels", funnels)

			if err != nil {
				fmt.Print(helpers.RedText("Config save error: " + err.Error()))
				time.Sleep(5 * time.Second)
				os.Exit(1)
			}

			os.Exit(0)
			return nil
		},
	},
}

func ManageConsole(user types.TestAuth, funnels types.FunnelList) {
	for {
		fmt.Println("")
		fmt.Println("")

		for key, action := range funnelActions {
			fmt.Println(key, "-", action.Name)
		}

		fmt.Println("")

		keyInput := helpers.GetInput("Select an option?", func(s string) bool {
			_, ok := funnelActions[s]

			if !ok {
				fmt.Print(helpers.RedText("Invalid option"))
				return false
			}

			return true
		})

		action, ok := funnelActions[keyInput]

		if !ok {
			fmt.Print(helpers.RedText("Invalid option"))
			continue
		}

		err := action.Action(&funnels)

		if err != nil {
			fmt.Print(helpers.RedText("Invalid option"))
			time.Sleep(5 * time.Second)
		}
	}
}

func portMan(funnels *types.FunnelList) error {
	port := helpers.GetInput("What port should the webserver run on?", func(s string) bool {
		// Check if port is a number
		_, err := strconv.Atoi(s)

		if err != nil {
			fmt.Fprint(os.Stderr, helpers.RedText("Invalid port number"))
			return false
		}

		return true
	})

	portNum, err := strconv.Atoi(port)

	if err != nil {
		return errors.New("invalid port number")
	}

	funnels.Port = portNum

	return nil
}

func setDomain(funnels *types.FunnelList) error {
	domain := helpers.GetInput("What domain/IP will the webhook be accessible from?", func(s string) bool {
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			return true
		}

		fmt.Print(helpers.RedText("Invalid domain. Must start with http:// or https://"))
		return false
	})

	funnels.Domain = domain

	return nil
}

func newFunnel(funnels *types.FunnelList) error {
	if funnels.Port == 0 || funnels.Domain == "" {
		fmt.Print(helpers.RedText("Please set a port and webhook domain ('P' and 'D') before adding a funnel"))
		return nil
	}

	authType := helpers.GetInput("Auth Type (bot/server)", func(s string) bool {
		if strings.ToLower(s) == "bot" || strings.ToLower(s) == "server" {
			return true
		} else {
			fmt.Print(helpers.RedText("Invalid auth type. Choose from bot, user or server"))
			return false
		}
	})

	var targetType types.TargetType

	switch strings.ToLower(authType) {
	case "bot":
		targetType = types.TargetTypeBot
	case "server":
		targetType = types.TargetTypeServer
	default:
		return errors.New("invalid target type")
	}

	targetID := helpers.GetInput("Target ID ["+authType+" ID, vanities are also supported]", func(s string) bool {
		return len(s) > 0
	})

	forwardTo := helpers.GetInput("Forward to?", func(s string) bool {
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			return true
		}

		fmt.Print(helpers.RedText("Invalid domain. Must start with http:// or https://"))
		return false
	})

	// Fetch entity
	switch targetType {
	case types.TargetTypeBot:
		resp, err := helpers.NewReq().Get("bots/" + targetID).Do()

		if err != nil {
			return errors.New("invalid fetching bot:" + err.Error())
		}

		if resp.Response.StatusCode == 404 {
			return errors.New("bot not found (404)")
		}

		if resp.Response.StatusCode != 200 {
			body, err := resp.Body()

			if err != nil {
				return errors.New("api error and body resolve error (status code " + strconv.Itoa(resp.Response.StatusCode) + ")")
			}

			return errors.New("api error (status code " + strconv.Itoa(resp.Response.StatusCode) + "): " + string(body))
		}

		var e types.FunnelBot

		err = resp.Json(&e)

		if err != nil {
			return errors.New("error occurred while parsing bot data: " + err.Error())
		}

		fmt.Print("Adding", helpers.BoldText(e.User.Username), "("+e.BotID+")", "to funnels")
		fmt.Print(helpers.BlueText("Updating webhook configuration for this bot..."))

		endpointId := crypto.RandString(32)
		webhookSecret := crypto.RandString(128)

		fmt.Print(helpers.BlueText("Domain: " + funnels.Domain + "/?id=" + endpointId))

		tBool := true

		pw := types.PatchBotWebhook{
			WebhookURL:    funnels.Domain + "/?id=" + endpointId,
			WebhookSecret: webhookSecret,
			WebhooksV2:    &tBool,
		}

		resp, err = helpers.NewReq().Patch("bots/" + e.BotID + "/webhook").Json(pw).Do()

		if err != nil {
			return errors.New("error occurred while updating webhook: " + err.Error())
		}

		if resp.Response.StatusCode != 200 {
			body, err := resp.Body()

			if err != nil {
				return errors.New("error occurred while parsing error when updating webhook: " + err.Error())
			}

			return errors.New("error occurred while updating webhook: " + string(body))
		}

		// Add to funnels
		funnels.Funnels = append(funnels.Funnels, types.WebhookFunnel{
			TargetType:    targetType,
			TargetID:      targetID,
			WebhookSecret: webhookSecret,
			EndpointID:    endpointId,
			Forward:       forwardTo,
		})

	case types.TargetTypeServer:
		return errors.New("server listing is not yet implemented on ibl itself")
	}

	return nil
}
