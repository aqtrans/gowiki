{ stdenv, lib, buildGoModule, git, makeWrapper, substituteAll, sassc, runCommand }:

buildGoModule rec {
  pname = "gowiki";
  #version = "0.0.1";
  
  # dynamic version based on git; https://blog.replit.com/nix_dynamic_version
  revision = runCommand "get-rev" {
          nativeBuildInputs = [ git ];
      } "GIT_DIR=${src}/.git git rev-parse --short HEAD | tr -d '\n' > $out";  

  version = "0" + builtins.readFile revision;

  src = ../.;

  nativeBuildInputs = [ makeWrapper sassc ];

  buildInputs = [ git ];

  ldflags = [ "-X main.appVersion=${version}" ];

  vendorSha256 = "0hbx4z3i33sprs3n5j86q9sahxq1zvi46mic4nj98wyhmhhqsmxa";

  runVend = false;

  deleteVendor = false;

  subPackages = [ "./" ];

  preBuild = ''
    sassc -t compressed -M scss/grid.scss assets/css/wiki.css
    substituteInPlace main.go --replace 'gitPath, err := exec.LookPath("git")' 'gitPath, err := exec.LookPath("${git}/bin/git")'
    substituteInPlace main_test.go --replace 'gitPath, err := exec.LookPath("git")' 'gitPath, err := exec.LookPath("${git}/bin/git")'
  '';

  meta = with lib; {
    description = "Simple wiki using Git underneath, written in Go";
    homepage = "https://github.com/aqtrans/gowiki";
    license = licenses.mit;
    maintainers = with maintainers; [ "aqtrans" ];
    platforms = platforms.linux;
  };
}
