/*
$(document).ready(function(){
  $("#savewiki").submit(function(event){
    event.preventDefault();
    //var editortxt = document.getElementsByName("editor")[0].value;
    //console.log(editortxt);
    var link = $(this).attr('action');
    //console.log( $(this).serialize() );
    //console.log( $(this).serializeArray() );
    $.post( link, $(this).serialize(), function(data){
        if(data.success){
          $(location).attr('href','/'+data.name);
          //$(".alerts").append("<div class=\"alert-box success\" data-alert>Wiki page successfully saved! <a style='color:#fff' href=/"+data.name+"><i class='fa fa-external-link'></i>[Page URL]</a><a class=\"close\">&times;</a></div>");
          //$("#savewiki")[0].reset();
          //$(document).foundation('alert','reflow');
        } else {
            $(".alerts").append("<div class=\"alert-box alert\" data-alert>Failed to save wiki page<a class=\"close\">&times;</a></div>");
            $("#alerts").append("<article><header><h3>Failed to save wiki page</h3>");
            $("#alerts").append("<label for=\"alert_modal\" class=\"close\">&times;</label></header>");
            $("#alerts").append("<section class=\"content\">Please check logs or try again.</section>");
            $("#alerts").append("<footer><label for=\"alert_modal\" class=\"button dangerous\">Okay</label></footer></article>");
            $( "#alert_modal" ).prop( "checked", true );
        }
      });
  });

  $("#login").submit(function(event){
    event.preventDefault();   
    $.post( "login", $( this ).serialize(), function(data){
      if(data.success){  
        $(location).attr('href', data.name);
        //$(".alerts").append("<div class=\"alert-box success\" data-alert>Successful login<a class=\"close\">&times;</a></div>");
        //$("#login-form").remove();
        //$(document).foundation('alert','reflow');
      } else {
        $( "#login_modal" ).prop( "checked", false );
        $("#alerts").append("<article><header><h3>Login failure</h3>");
        $("#alerts").append("<label for=\"alert_modal\" class=\"close\">&times;</label></header>");
        $("#alerts").append("<section class=\"content\">Please try to login again.</section>");
        $("#alerts").append("<footer><label for=\"alert_modal\" class=\"button dangerous\">Okay</label></footer></article>");
        $( "#alert_modal" ).prop( "checked", true );
        $("#alert_modal :checkbox").click(function() {
                            var $this = $(this);
                            if ($this.is(':checked')) {
                                
                            } else {
                                location.reload();
                            }
                        });
      }
    });
  });

});
*/