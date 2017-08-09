var gulp = require('gulp');
var $    = require('gulp-load-plugins')();
var sourcemaps = require('gulp-sourcemaps');

gulp.task('sass', function() {
  return gulp.src('wiki.scss')
    .pipe(sourcemaps.init())
    .pipe($.sass({
      includePaths: sassPaths,
      outputStyle: 'compressed' // if css compressed **file size**
    })
      .on('error', $.sass.logError))
    .pipe($.autoprefixer({
      browsers: ['last 2 versions', 'ie >= 9']
    }))
    .pipe(sourcemaps.write('./'))
    .pipe(gulp.dest('../assets/css/'));
});

gulp.task('default', ['sass'], function() {
  gulp.watch(['*.scss'], ['sass']);
});
