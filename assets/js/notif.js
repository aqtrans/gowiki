function notif() {
    const notificationBar = document.getElementById('notification');
    notificationBar.classList.remove("active");
    notificationBar.addEventListener('transitionend', () => {
        notificationBar.remove();
    })
}

document.addEventListener('DOMContentLoaded', function () {
    var closebutton = document.getElementById('close-button');
    if (closebutton){
        closebutton.addEventListener('click', notif);
    }
});
  