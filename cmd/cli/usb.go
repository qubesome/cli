package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/qubesome/cli/internal/runners/util/usb"
	"github.com/urfave/cli/v3"
)

func usbCommand() *cli.Command {
	cmd := &cli.Command{
		Name:   "usb",
		Hidden: true,
		Usage:  "lists USB devices detected on the host",
		Description: `Lists the USB devices detected on the host, showing the
vendor:product identifier, product name and the /dev paths that would be made
available to a workload. Use the vendor:product identifier in a workload's
usbDevices configuration.`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			devices, err := usb.List()
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tPRODUCT\tPATHS")
			for _, d := range devices {
				fmt.Fprintf(w, "%s:%s\t%s\t%s\n",
					d.VendorID, d.ProductID, d.Product, strings.Join(d.Paths, ", "))
			}
			return w.Flush()
		},
	}
	return cmd
}
