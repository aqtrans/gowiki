function notif() {
    document.getElementById('notification').classList.remove("active");
}

document.addEventListener('DOMContentLoaded', function () {
    document.getElementById('close-button')
      .addEventListener('click', notif);
});
  