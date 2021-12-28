{ stdenv, lib, buildGoModule, git, makeWrapper, substituteAll }:

buildGoModule rec {
  pname = "gowiki";
  version = "0.0.1";

  src = ./.;

  nativeBuildInputs = [ makeWrapper ];

  buildInputs = [ git ];

  vendorSha256 = null;

  runVend = true;

  deleteVendor = false;

  subPackages = [ "./" ];

  preBuild = ''
    substituteInPlace main.go --replace 'gitPath, err := exec.LookPath("git")' 'gitPath, err := exec.LookPath("${git}/bin/git")'
    substituteInPlace main_test.go --replace 'gitPath, err := exec.LookPath("git")' 'gitPath, err := exec.LookPath("${git}/bin/git")'
  '';

  meta = with lib; {
    description = "Simple wiki using Git underneath, written in Go";
    homepage = "https://github.com/aqtrans/gowiki";
    license = licenses.mit;
    maintainers = with maintainers; [ aqtrans ];
    platforms = platforms.linux;
  };
}
