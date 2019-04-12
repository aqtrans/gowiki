function notif() {
    document.getElementById('notification').classList.remove("active");
}

document.addEventListener('DOMContentLoaded', function () {
    var closebutton = document.getElementById('close-button');
    if (closebutton){
        closebutton.addEventListener('click', notif);
    }
});
  