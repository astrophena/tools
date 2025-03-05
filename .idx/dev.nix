# To learn more about how to use Nix to configure your environment
# see: https://developers.google.com/idx/guides/customize-idx-env.
{ pkgs, ... }: {
  # Which nixpkgs channel to use.
  channel = "unstable";

  # Use https://search.nixos.org/packages to find packages.
  packages = [
    pkgs.go_1_24
    pkgs.gopls
    pkgs.go-tools
  ];

  idx = {
    # Search for the extensions you want on https://open-vsx.org/ and use "publisher.id".
    extensions = [
      "golang.Go"
    ];

    workspace = {
      # Runs when a workspace is first created.
      onCreate = {
        deps = "go mod download";
      };
    };
  };
}
